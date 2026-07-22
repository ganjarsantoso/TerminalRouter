package quota

import (
	"context"
	"sync"
	"time"
)

// Collector is the interface for provider-specific quota collection adapters.
// Each collector knows how to query one provider type for quota/rate-limit data.
type Collector interface {
	// Name returns a human-readable name for this collector.
	Name() string
	// Supports reports whether this collector can handle the given provider type.
	Supports(providerType string) bool
	// Collect queries the provider for current quota state.
	Collect(ctx context.Context, account ProviderAccountContext) (*ProviderQuotaSnapshot, error)
}

// ProviderAccountContext provides the collector with connection details.
type ProviderAccountContext struct {
	ProviderID    string
	AccountID     string
	ProviderType  string
	BaseURL       string
	CredentialRef string
	Credential    string // resolved credential value
	ModelPattern  string
}

// Registry manages registered quota collectors.
type Registry struct {
	mu         sync.RWMutex
	collectors []Collector
}

// NewRegistry creates an empty collector registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a collector to the registry.
func (r *Registry) Register(c Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors = append(r.collectors, c)
}

// Collectors returns all registered collectors.
func (r *Registry) Collectors() []Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Collector, len(r.collectors))
	copy(out, r.collectors)
	return out
}

// FindFor returns collectors that support the given provider type.
func (r *Registry) FindFor(providerType string) []Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var matched []Collector
	for _, c := range r.collectors {
		if c.Supports(providerType) {
			matched = append(matched, c)
		}
	}
	return matched
}

// Collect runs all matching collectors for an account and returns merged results.
func (r *Registry) Collect(ctx context.Context, pctx ProviderAccountContext) (*ProviderQuotaSnapshot, error) {
	collectors := r.FindFor(pctx.ProviderType)
	if len(collectors) == 0 {
		return &ProviderQuotaSnapshot{
			ProviderID: pctx.ProviderID,
			AccountID:  pctx.AccountID,
			ObservedAt: time.Now().UTC(),
			Source:     SourceUnknown,
			Error:      "no collector available for provider type " + pctx.ProviderType,
		}, nil
	}

	// Use the first matching collector; in future, merge results.
	snap, err := collectors[0].Collect(ctx, pctx)
	if err != nil {
		return &ProviderQuotaSnapshot{
			ProviderID: pctx.ProviderID,
			AccountID:  pctx.AccountID,
			ObservedAt: time.Now().UTC(),
			Source:     SourceUnknown,
			Error:      err.Error(),
		}, err
	}
	return snap, nil
}

// HeaderCollector extracts quota data from provider response headers.
// It is the simplest collector and works for OpenAI and Anthropic rate-limit headers.
type HeaderCollector struct{}

func (c *HeaderCollector) Name() string         { return "header_collector" }
func (c *HeaderCollector) Supports(string) bool { return true } // works for all providers

func (c *HeaderCollector) Collect(_ context.Context, _ ProviderAccountContext) (*ProviderQuotaSnapshot, error) {
	// Header collection happens at request time via ExtractQuotaHeaders.
	// This collector is a placeholder; actual header extraction is wired
	// into the provider adapter response path.
	return &ProviderQuotaSnapshot{
		Source: SourceProviderHeader,
	}, nil
}

// LocalCollector aggregates usage from TermRouter's own request_log.
type LocalCollector struct {
	UsageFunc func(providerID, accountID string, from, to time.Time) (*LocalUsage, error)
}

// LocalUsage is the output of local usage aggregation.
type LocalUsage struct {
	Requests     int
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	EstimatedUSD float64
}

func (c *LocalCollector) Name() string         { return "local_collector" }
func (c *LocalCollector) Supports(string) bool { return true }

func (c *LocalCollector) Collect(ctx context.Context, pctx ProviderAccountContext) (*ProviderQuotaSnapshot, error) {
	if c.UsageFunc == nil {
		return &ProviderQuotaSnapshot{
			ProviderID: pctx.ProviderID,
			AccountID:  pctx.AccountID,
			Source:     SourceLocalEstimated,
			Error:      "local usage function not configured",
		}, nil
	}

	now := time.Now().UTC()
	windowStart := now.Add(-24 * time.Hour) // default: last 24 hours

	usage, err := c.UsageFunc(pctx.ProviderID, pctx.AccountID, windowStart, now)
	if err != nil {
		return &ProviderQuotaSnapshot{
			ProviderID: pctx.ProviderID,
			AccountID:  pctx.AccountID,
			Source:     SourceLocalAuthoritative,
			Error:      err.Error(),
		}, err
	}

	return &ProviderQuotaSnapshot{
		ProviderID: pctx.ProviderID,
		AccountID:  pctx.AccountID,
		ObservedAt: now,
		Source:     SourceLocalAuthoritative,
		Windows: []QuotaWindowState{{
			ProviderID:    pctx.ProviderID,
			AccountID:     pctx.AccountID,
			Dimension:     DimTotalTokens,
			Unit:          UnitTokens,
			WindowType:    WindowRolling,
			WindowStart:   windowStart,
			Used:          float64(usage.TotalTokens),
			LocalUsed:     float64(usage.TotalTokens),
			Source:        SourceLocalAuthoritative,
			Confidence:    1.0,
			LastUpdatedAt: now,
			StaleAfter:    now.Add(5 * time.Minute),
		}},
	}, nil
}
