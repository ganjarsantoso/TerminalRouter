package optimization

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/config"
)

// RecordStatus indicates the completion state of an optimization record.
type RecordStatus string

const (
	// RecordPending means the optimization record was created but the finalization
	// outcome has not been determined yet.
	RecordPending RecordStatus = "pending"
	// RecordComplete means the stream finished normally and actuals were captured.
	RecordComplete RecordStatus = "complete"
	// RecordIncompleteClientDisconnect means the client disconnected before the stream finished.
	RecordIncompleteClientDisconnect RecordStatus = "incomplete_client_disconnect"
	// RecordIncompleteUpstreamError means the upstream returned an error.
	RecordIncompleteUpstreamError RecordStatus = "incomplete_upstream_error"
	// RecordCancelled means the request context was cancelled.
	RecordCancelled RecordStatus = "cancelled"
)

// Record is the persisted, privacy-conscious optimization decision record. It
// contains no raw prompt text. Cache-related fields use distinct labels to
// separate estimates from actuals.
type Record struct {
	RequestID                  string
	RouteName                  string
	ClientKeyID                string
	ProviderID                 string
	ModelID                    string
	ModeRequested              string
	ModeApplied                string
	LUIVersion                 string
	Renderer                   string
	EstimatorsJSON             string
	OptimizersJSON             string
	InputTokensBefore          int
	InputTokensAfterEstimated  int
	ProviderInputTokensActual  int
	ProviderOutputTokensActual int
	CacheStatus                string // prefix_stabilized | cache_opportunity_estimated | cache_requested | cache_reported_by_provider
	CacheOpportunityTokensEst  int    // estimated cache opportunity (prefix stabilization)
	CacheReadTokensActual      int    // actual cache-read tokens from provider response
	CacheWriteTokensActual     int    // actual cache-write tokens from provider response
	CacheSource                string // "" | provider_name | stabilization
	CompressionInputTokens     int
	CompressionOutputTokens    int
	GrossSavingUSD             float64
	OptimizerCostUSD           float64
	NetSavingUSD               float64
	AddedLatencyMs             int
	LossClass                  string
	Bypassed                   bool
	BypassReason               string
	QualityStatus              string
	Status                     RecordStatus
	CreatedAt                  time.Time
}

// RecordFromResult builds a persistence record from a finished optimization.
func RecordFromResult(oc OptimizationContext, res *OptimizationResult) Record {
	var ests []string
	ests = append(ests, "fallback-chars")
	estJSON, _ := json.Marshal(ests)
	var opts []string
	for _, a := range res.Actions {
		opts = append(opts, string(a.Kind))
	}
	optJSON, _ := json.Marshal(opts)
	cacheStatus := ""
	cacheSource := ""
	cacheOppEst := res.ExpectedCachedTokens
	if cacheOppEst > 0 {
		cacheStatus = "cache_opportunity_estimated"
		cacheSource = "stabilization"
	}
	r := Record{
		RequestID:                 oc.RequestID,
		RouteName:                 oc.RouteName,
		ClientKeyID:               oc.ClientKeyID,
		ProviderID:                oc.ProviderID,
		ModelID:                   oc.ModelID,
		ModeRequested:             string(res.ModeRequested),
		ModeApplied:               string(res.ModeApplied),
		LUIVersion:                res.LUIVersion,
		Renderer:                  res.LUIRenderer,
		EstimatorsJSON:            string(estJSON),
		OptimizersJSON:            string(optJSON),
		InputTokensBefore:         res.InputTokensBefore,
		InputTokensAfterEstimated: res.InputTokensEstimated,
		CacheStatus:               cacheStatus,
		CacheOpportunityTokensEst: cacheOppEst,
		CacheSource:               cacheSource,
		CompressionInputTokens:    res.CompressionTokens,
		GrossSavingUSD:            res.EstimatedGrossSavingUSD,
		OptimizerCostUSD:          res.EstimatedOptimizerCost,
		NetSavingUSD:              res.EstimatedNetSavingUSD,
		AddedLatencyMs:            int(res.AddedLatency.Milliseconds()),
		LossClass:                 string(res.LossClass),
		Bypassed:                  res.Bypassed,
		BypassReason:              res.BypassReason,
		Status:                    RecordPending,
	}
	return r
}

// FinalizeWithActuals fills provider-reported actuals once a response returns.
// Cache actuals are stored in separate fields from estimates.
func (r *Record) FinalizeWithActuals(inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int) {
	r.ProviderInputTokensActual = inputTokens
	r.ProviderOutputTokensActual = outputTokens
	if cacheReadTokens >= 0 {
		r.CacheReadTokensActual = cacheReadTokens
		r.CacheStatus = "cache_reported_by_provider"
	}
	if cacheWriteTokens >= 0 {
		r.CacheWriteTokensActual = cacheWriteTokens
	}
}

// InsertOptimizationRecord is a helper used by storage adapters implementing the
// optimization.Store interface.
func (r Record) Insert(ctx context.Context, s Store) error { return s.InsertOptimizationRecord(ctx, r) }

// DefaultConfig returns the engine default optimization configuration.
func DefaultConfig() config.OptimizationConfig {
	return config.DefaultOptimization()
}

// StreamFinalizer ensures optimization record finalization happens exactly once,
// regardless of whether the stream completes normally, errors, or is cancelled.
// The caller passes the terminal status to Finalize, avoiding the data race that
// would exist if status were set separately.
type StreamFinalizer struct {
	once   sync.Once
	engine *Engine
	record *Record
}

// NewStreamFinalizer creates a finalizer that will persist the record exactly
// once when Finalize is called.
func NewStreamFinalizer(engine *Engine, rec *Record) *StreamFinalizer {
	return &StreamFinalizer{
		engine: engine,
		record: rec,
	}
}

// Finalize completes the optimization record with the given terminal status and
// provider-reported actuals, then persists it. It is safe to call multiple times;
// only the first call takes effect.
func (sf *StreamFinalizer) Finalize(ctx context.Context, status RecordStatus, inputTokens, outputTokens, cachedTokens int) {
	sf.once.Do(func() {
		if sf.engine == nil || sf.record == nil {
			return
		}
		sf.record.Status = status
		sf.engine.FinalizeAndPersist(ctx, sf.record, inputTokens, outputTokens, cachedTokens)
	})
}
