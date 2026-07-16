package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/provider"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/storage"
)

const (
	defaultMaxAttempts      = 3
	circuitFailureThreshold = 5
	initialCooldown         = 30 * time.Second
)

// Result is the outcome of a non-streaming execution.
type Result struct {
	Response       *normalization.NormalizedResponse
	ProviderID     string
	UpstreamModel  string
	Attempt        int
	FallbackReason string
	Latency        time.Duration
}

// StreamResult holds a committed stream from a successful attempt.
type StreamResult struct {
	Stream         provider.EventStream
	ProviderID     string
	UpstreamModel  string
	Attempt        int
	FallbackReason string
	// OnCommit is called when first client-visible content is seen (optional hook).
	PublicModel string
}

// Coordinator runs retry/fallback/circuit logic.
type Coordinator struct {
	Registry *provider.Registry
	Creds    *credentials.Manager
	Store    *storage.Store
	Log      *observability.Logger
}

func New(reg *provider.Registry, creds *credentials.Manager, store *storage.Store, log *observability.Logger) *Coordinator {
	return &Coordinator{Registry: reg, Creds: creds, Store: store, Log: log}
}

// Execute runs a non-streaming request with fallback before any response is returned.
func (c *Coordinator) Execute(ctx context.Context, req *normalization.NormalizedRequest, plan *router.Plan) (*Result, error) {
	var lastErr error
	var fallbackReason string
	max := defaultMaxAttempts
	if len(plan.Attempts) < max {
		max = len(plan.Attempts)
	}
	// Also allow retries within same target for transient errors — cap total attempts.
	totalBudget := max * 2
	attemptNum := 0

	for i, att := range plan.Attempts {
		if attemptNum >= totalBudget {
			break
		}
		if !c.eligible(ctx, att.ProviderID) {
			fallbackReason = "circuit_open"
			continue
		}
		// retries for same target
		for retry := 0; retry < 2; retry++ {
			attemptNum++
			start := time.Now()
			resp, err := c.tryOnce(ctx, req, att)
			lat := time.Since(start)
			if err == nil {
				c.recordSuccess(ctx, att.ProviderID, lat)
				return &Result{
					Response:       resp,
					ProviderID:     att.ProviderID,
					UpstreamModel:  att.Model,
					Attempt:        attemptNum,
					FallbackReason: fallbackReason,
					Latency:        lat,
				}, nil
			}
			lastErr = err
			ne := asNormErr(err)
			c.recordFailure(ctx, att.ProviderID, ne)
			if c.Log != nil {
				c.Log.Warn("upstream attempt failed",
					"provider", att.ProviderID,
					"model", att.Model,
					"attempt", attemptNum,
					"error", observability.Redact(err.Error()),
				)
			}
			if ne != nil && !ne.Retryable {
				// non-retryable on this target; for 401 may try next target
				if ne.Code == normalization.ErrProviderAuth || ne.Code == normalization.ErrRateLimited || ne.Code == normalization.ErrProviderUnavailable {
					fallbackReason = ne.Code
					break // next target
				}
				return nil, ne
			}
			// retryable: backoff then maybe next target
			if retry == 0 && ne != nil && ne.Retryable {
				if err := sleepBackoff(ctx, retry); err != nil {
					return nil, err
				}
				continue
			}
			fallbackReason = "retryable_failure"
			break
		}
		// only use further targets if strategy is fallback
		if plan.Strategy != "fallback" && i == 0 {
			break
		}
		_ = i
	}
	if lastErr == nil {
		return nil, &normalization.Error{
			Code: normalization.ErrProviderUnavailable,
			Message: "no healthy eligible target",
			HTTPStatus: 503,
		}
	}
	if ne := asNormErr(lastErr); ne != nil {
		return nil, ne
	}
	return nil, &normalization.Error{
		Code: normalization.ErrProviderUnavailable,
		Message: lastErr.Error(),
		HTTPStatus: 503,
		Retryable: true,
	}
}

// Stream opens a stream with pre-commit fallback. The caller must drive the stream;
// if the stream fails before Commit, the caller should call Stream again is NOT supported —
// instead ExecuteStream handles pre-commit fallback by peeking.
func (c *Coordinator) ExecuteStream(ctx context.Context, req *normalization.NormalizedRequest, plan *router.Plan) (*StreamResult, error) {
	var lastErr error
	var fallbackReason string
	attemptNum := 0

	for i, att := range plan.Attempts {
		if !c.eligible(ctx, att.ProviderID) {
			fallbackReason = "circuit_open"
			continue
		}
		attemptNum++
		stream, err := c.openStream(ctx, req, att)
		if err != nil {
			lastErr = err
			ne := asNormErr(err)
			c.recordFailure(ctx, att.ProviderID, ne)
			if ne != nil && !ne.Retryable && ne.Code != normalization.ErrProviderAuth && ne.Code != normalization.ErrRateLimited {
				return nil, ne
			}
			fallbackReason = "open_failed"
			if plan.Strategy != "fallback" {
				break
			}
			continue
		}

		// Peek until commit or error (buffer non-commit events)
		buffered, committed, err := peekUntilCommit(stream)
		if err != nil {
			_ = stream.Close()
			lastErr = err
			c.recordFailure(ctx, att.ProviderID, asNormErr(err))
			fallbackReason = "pre_commit_failure"
			if plan.Strategy != "fallback" {
				break
			}
			continue
		}
		if !committed {
			// Stream ended without content — treat as soft failure for fallback
			_ = stream.Close()
			lastErr = fmt.Errorf("empty stream from %s", att.ProviderID)
			fallbackReason = "empty_stream"
			if plan.Strategy == "fallback" && i+1 < len(plan.Attempts) {
				continue
			}
		}

		c.recordSuccess(ctx, att.ProviderID, 0)
		return &StreamResult{
			Stream:         &replayStream{buf: buffered, inner: stream},
			ProviderID:     att.ProviderID,
			UpstreamModel:  att.Model,
			Attempt:        attemptNum,
			FallbackReason: fallbackReason,
			PublicModel:    plan.PublicModel,
		}, nil
	}

	if lastErr == nil {
		return nil, &normalization.Error{
			Code: normalization.ErrProviderUnavailable,
			Message: "no healthy eligible target",
			HTTPStatus: 503,
		}
	}
	if ne := asNormErr(lastErr); ne != nil {
		return nil, ne
	}
	return nil, lastErr
}

func (c *Coordinator) tryOnce(ctx context.Context, req *normalization.NormalizedRequest, att router.Attempt) (*normalization.NormalizedResponse, error) {
	adapter, ok := c.Registry.Get(att.Config.Type)
	if !ok {
		return nil, &normalization.Error{
			Code: normalization.ErrInternal,
			Message: fmt.Sprintf("no adapter for provider type %q", att.Config.Type),
			HTTPStatus: 500,
		}
	}
	cred, err := c.Creds.Resolve(att.Config.CredentialRef)
	if err != nil {
		return nil, &normalization.Error{
			Code: normalization.ErrProviderAuth,
			Message: fmt.Sprintf("credential resolve failed for %s: %v", att.ProviderID, err),
			HTTPStatus: 502,
		}
	}
	target := router.ToProviderTarget(att)
	resp, err := adapter.Execute(ctx, req, target, cred)
	if err != nil {
		if ne, ok := err.(*normalization.Error); ok {
			return nil, ne
		}
		return nil, adapter.ClassifyError(0, nil, err)
	}
	resp.Model = req.RequestedModel
	if req.ResolvedAlias != "" {
		resp.Model = req.ResolvedAlias
	}
	return resp, nil
}

func (c *Coordinator) openStream(ctx context.Context, req *normalization.NormalizedRequest, att router.Attempt) (provider.EventStream, error) {
	adapter, ok := c.Registry.Get(att.Config.Type)
	if !ok {
		return nil, &normalization.Error{
			Code: normalization.ErrInternal,
			Message: fmt.Sprintf("no adapter for provider type %q", att.Config.Type),
			HTTPStatus: 500,
		}
	}
	cred, err := c.Creds.Resolve(att.Config.CredentialRef)
	if err != nil {
		return nil, &normalization.Error{
			Code: normalization.ErrProviderAuth,
			Message: fmt.Sprintf("credential resolve failed for %s: %v", att.ProviderID, err),
			HTTPStatus: 502,
		}
	}
	target := router.ToProviderTarget(att)
	stream, err := adapter.Stream(ctx, req, target, cred)
	if err != nil {
		if ne, ok := err.(*normalization.Error); ok {
			return nil, ne
		}
		return nil, adapter.ClassifyError(0, nil, err)
	}
	return stream, nil
}

func (c *Coordinator) eligible(ctx context.Context, providerID string) bool {
	if c.Store == nil {
		return true
	}
	h, err := c.Store.GetProviderHealth(ctx, providerID)
	if err != nil || h == nil {
		return true
	}
	now := time.Now()
	switch h.CircuitState {
	case storage.CircuitOpen:
		if h.CooldownUntil != nil && now.After(*h.CooldownUntil) {
			// transition to half-open
			h.CircuitState = storage.CircuitHalfOpen
			_ = c.Store.UpsertProviderHealth(ctx, *h)
			return true
		}
		return false
	case storage.CircuitHalfOpen:
		return true
	default:
		return true
	}
}

func (c *Coordinator) recordSuccess(ctx context.Context, providerID string, lat time.Duration) {
	if c.Store == nil {
		return
	}
	now := time.Now().UTC()
	_ = c.Store.UpsertProviderHealth(ctx, storage.ProviderHealth{
		ProviderID:          providerID,
		CircuitState:        storage.CircuitClosed,
		ConsecutiveFailures: 0,
		LastLatencyMs:       int(lat.Milliseconds()),
		LastSuccessAt:       &now,
		UpdatedAt:           now,
	})
}

func (c *Coordinator) recordFailure(ctx context.Context, providerID string, ne *normalization.Error) {
	if c.Store == nil || ne == nil || !ne.Retryable {
		return
	}
	h, _ := c.Store.GetProviderHealth(ctx, providerID)
	if h == nil {
		h = &storage.ProviderHealth{ProviderID: providerID, CircuitState: storage.CircuitClosed}
	}
	now := time.Now().UTC()
	h.ConsecutiveFailures++
	h.LastFailureAt = &now
	h.LastError = ne.Code
	if h.ConsecutiveFailures >= circuitFailureThreshold {
		h.CircuitState = storage.CircuitOpen
		cd := now.Add(initialCooldown)
		h.CooldownUntil = &cd
	}
	h.UpdatedAt = now
	_ = c.Store.UpsertProviderHealth(ctx, *h)
}

func asNormErr(err error) *normalization.Error {
	if err == nil {
		return nil
	}
	var ne *normalization.Error
	if errors.As(err, &ne) {
		return ne
	}
	if e, ok := err.(*normalization.Error); ok {
		return e
	}
	return nil
}

func sleepBackoff(ctx context.Context, retry int) error {
	base := time.Duration(100*(1<<retry)) * time.Millisecond
	jitter := time.Duration(rand.Intn(100)) * time.Millisecond
	t := time.NewTimer(base + jitter)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func peekUntilCommit(stream provider.EventStream) ([]normalization.StreamEvent, bool, error) {
	var buf []normalization.StreamEvent
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			return buf, false, nil
		}
		if err != nil {
			return buf, false, err
		}
		if ev.Type == normalization.EventError && ev.Error != nil {
			return buf, false, ev.Error
		}
		buf = append(buf, ev)
		if ev.Commit {
			return buf, true, nil
		}
		// message_stop without commit
		if ev.Type == normalization.EventMessageStop {
			return buf, false, nil
		}
	}
}

type replayStream struct {
	buf   []normalization.StreamEvent
	inner provider.EventStream
	i     int
}

func (r *replayStream) Recv() (normalization.StreamEvent, error) {
	if r.i < len(r.buf) {
		ev := r.buf[r.i]
		r.i++
		return ev, nil
	}
	return r.inner.Recv()
}

func (r *replayStream) Close() error {
	return r.inner.Close()
}
