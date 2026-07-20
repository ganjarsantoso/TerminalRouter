package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/config"
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
	// Cfg provides pricing lookups for spend enforcement. May be nil when
	// pricing is not configured (in which case cost budgets cannot be enforced
	// and unpriced routes are rejected for portable/public keys).
	Cfg *config.Config

	mu       sync.Mutex
	reserved map[string]*spendReservation
}

// PolicyContext carries the authenticated client key and deployment posture
// into the execution boundary so the coordinator can perform authoritative
// spend admission (per-route pricing + worst-case reservation + daily budget).
type PolicyContext struct {
	ClientKey    *storage.ClientKey
	PublicHosting bool
}

// spendReservation tracks the not-yet-finalized worst-case estimated spend of
// a single in-flight request. The Coordinator's reserved map holds the
// accumulated total per client key; this per-request handle records only the
// amount contributed by its own request so release subtracts the right value.
type spendReservation struct {
	keyID   string
	costUSD float64
}

func New(reg *provider.Registry, creds *credentials.Manager, store *storage.Store, log *observability.Logger) *Coordinator {
	return &Coordinator{
		Registry: reg,
		Creds:    creds,
		Store:    store,
		Log:      log,
		reserved: map[string]*spendReservation{},
	}
}

// Execute runs a non-streaming request with fallback before any response is returned.
func (c *Coordinator) Execute(ctx context.Context, req *normalization.NormalizedRequest, plan *router.Plan, policy PolicyContext) (*Result, error) {
	resv, aerr := c.admit(ctx, req, plan, policy)
	if aerr != nil {
		return nil, aerr
	}
	defer c.releaseSpend(resv)

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
func (c *Coordinator) ExecuteStream(ctx context.Context, req *normalization.NormalizedRequest, plan *router.Plan, policy PolicyContext) (*StreamResult, error) {
	resv, aerr := c.admit(ctx, req, plan, policy)
	if aerr != nil {
		return nil, aerr
	}
	defer c.releaseSpend(resv)

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

// admit performs authoritative spend admission before any provider call:
//  1. per-route pricing gate — every potentially-executable attempt must have a
//     configured price; otherwise a spend-enforced portable/public key is
//     rejected with ErrUnpriced (non-retryable, no fallback).
//  2. worst-case reservation + daily budget check — sum of the maximum estimated
//     cost over every attempt that could execute (covers fallback/retry) is
//     reserved against the daily spend budget (completed usage + active
//     reservations). Store-unavailable is fail-closed for portable/public keys.
//
// It returns a spend reservation to be released when the request completes.
func (c *Coordinator) admit(ctx context.Context, req *normalization.NormalizedRequest, plan *router.Plan, policy PolicyContext) (*spendReservation, error) {
	key := policy.ClientKey
	if key == nil || c.Cfg == nil {
		// No spend policy applies (e.g. Console Playground without a client key,
		// or pricing unconfigured). The Limiter still enforces request limits.
		return &spendReservation{}, nil
	}
	if key.DailyEstimatedCostUSD == nil || *key.DailyEstimatedCostUSD <= 0 {
		return &spendReservation{}, nil
	}
	failClosed := key.Portable || policy.PublicHosting
	if !failClosed {
		// Local, non-portable key with a cost budget: documented policy is to
		// fail open on pricing gaps; only portable/public keys are fail-closed.
		return &spendReservation{}, nil
	}

	// 1. Per-route pricing gate across every executable attempt.
	for _, att := range plan.Attempts {
		if _, ok := c.Cfg.LookupPrice(att.ProviderID, att.Model); !ok {
			c.logUnpriced(ctx, policy, plan, att.ProviderID, att.Model)
			return nil, &normalization.Error{
				Code:       normalization.ErrUnpriced,
				Message:    "The selected route contains a provider/model without configured pricing.",
				HTTPStatus: 402,
				Retryable:  false,
			}
		}
	}

	// 2. Worst-case reservation over all potentially billable attempts.
	estIn := estimateInputTokens(req)
	maxOut := maxOutputTokens(req)
	var worst float64
	for _, att := range plan.Attempts {
		if cost, ok := c.Cfg.ComputeCost(att.ProviderID, att.Model, estIn, maxOut); ok {
			worst += cost
		}
	}

	// Daily budget check: completed usage + active reservations + this request's
	// worst-case must stay within budget.
	dayStart := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), time.Now().UTC().Day(), 0, 0, 0, 0, time.UTC)
	if c.Store != nil {
		usage, err := c.Store.UsageForKeySince(ctx, key.ID, dayStart)
		if err != nil {
			// Usage policy unreadable: fail closed for portable/public keys.
			c.logSpendExceeded(ctx, policy, plan, worst)
			return nil, &normalization.Error{
				Code:       normalization.ErrQuotaUnavailable,
				Message:    "usage policy is temporarily unavailable",
				HTTPStatus: 503,
				Retryable:  false,
			}
		}
		reserved := c.currentReservedSpend(key.ID)
		if usage.EstimatedUSD+reserved+worst >= *key.DailyEstimatedCostUSD {
			c.logSpendExceeded(ctx, policy, plan, worst)
			return nil, &normalization.Error{
				Code:       normalization.ErrRateLimited,
				Message:    "daily estimated-spend budget exceeded for this client key",
				HTTPStatus: 429,
				Retryable:  false,
			}
		}
	}

	resv := c.reserveSpend(key.ID, worst)
	return resv, nil
}

// reserveSpend records an in-flight worst-case spend for the key and returns a
// per-request handle carrying only this request's contribution.
func (c *Coordinator) reserveSpend(keyID string, costUSD float64) *spendReservation {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.reserved[keyID]
	if cur == nil {
		cur = &spendReservation{}
		c.reserved[keyID] = cur
	}
	cur.costUSD += costUSD
	return &spendReservation{keyID: keyID, costUSD: costUSD}
}

// releaseSpend removes a request's contribution once it completes.
func (c *Coordinator) releaseSpend(resv *spendReservation) {
	if resv == nil || resv.keyID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.reserved[resv.keyID]
	if !ok {
		return
	}
	cur.costUSD -= resv.costUSD
	if cur.costUSD <= 0 {
		delete(c.reserved, resv.keyID)
	}
}

// currentReservedSpend returns the sum of active in-flight reservations for a key.
func (c *Coordinator) currentReservedSpend(keyID string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cur, ok := c.reserved[keyID]; ok {
		return cur.costUSD
	}
	return 0
}

// estimateInputTokens derives a conservative input-token estimate from the
// normalized request when no provider tokenizer is available. It uses a
// character-based approximation with a safety margin and is marked as an
// estimate only.
func estimateInputTokens(req *normalization.NormalizedRequest) int {
	chars := len(req.System)
	for _, m := range req.Messages {
		chars += len(m.Content) + len(m.Role)*4
	}
	// ~3.3 chars/token baseline, with a 1.20 safety margin.
	est := int(float64(chars) / 3.0 * 1.20)
	if est < 1 {
		est = 1
	}
	return est
}

// maxOutputTokens returns the requested maximum output tokens, or a conservative
// default when the client did not specify one.
func maxOutputTokens(req *normalization.NormalizedRequest) int {
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens > 0 {
		return *req.MaxOutputTokens
	}
	return 4096
}

func (c *Coordinator) logUnpriced(ctx context.Context, policy PolicyContext, plan *router.Plan, providerID, model string) {
	if c.Log == nil {
		return
	}
	c.Log.Warn("security_event",
		"event", "unpriced_route",
		"request_id", observability.RequestIDFrom(ctx),
		"client_key_id", clientKeyID(policy.ClientKey),
		"public_alias", plan.PublicModel,
		"route", plan.RouteName,
		"unpriced_provider", providerID,
		"unpriced_model", model,
	)
}

func (c *Coordinator) logSpendExceeded(ctx context.Context, policy PolicyContext, plan *router.Plan, costUSD float64) {
	if c.Log == nil {
		return
	}
	c.Log.Warn("security_event",
		"event", "daily_spend_budget_exceeded",
		"request_id", observability.RequestIDFrom(ctx),
		"client_key_id", clientKeyID(policy.ClientKey),
		"public_alias", plan.PublicModel,
		"route", plan.RouteName,
		"estimated_cost_usd", costUSD,
	)
}

func clientKeyID(k *storage.ClientKey) string {
	if k == nil {
		return ""
	}
	return k.ID
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
