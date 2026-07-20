package execution

import (
	"context"
	"io"
	"testing"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/provider"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/storage"
)

// fakeAdapter records calls and returns a trivial successful result/stream.
type fakeAdapter struct {
	calls int
}

func (f *fakeAdapter) Type() string { return "fake" }
func (f *fakeAdapter) Capabilities(config.ProviderConfig) provider.CapabilitySet {
	return provider.CapabilitySet{}
}
func (f *fakeAdapter) Validate(ctx context.Context, conn config.ProviderConfig, credential string) error {
	return nil
}
func (f *fakeAdapter) ListModels(ctx context.Context, conn config.ProviderConfig, credential string) ([]provider.Model, error) {
	return nil, nil
}
func (f *fakeAdapter) Execute(ctx context.Context, req *normalization.NormalizedRequest, target provider.Target, credential string) (*normalization.NormalizedResponse, error) {
	f.calls++
	return &normalization.NormalizedResponse{
		Content:    []normalization.ContentBlock{{Type: normalization.ContentText, Text: "ok"}},
		StopReason: normalization.StopEndTurn,
	}, nil
}
func (f *fakeAdapter) Stream(ctx context.Context, req *normalization.NormalizedRequest, target provider.Target, credential string) (provider.EventStream, error) {
	f.calls++
	return &fakeStream{}, nil
}
func (f *fakeAdapter) ClassifyError(status int, body []byte, err error) *normalization.Error {
	return nil
}

type fakeStream struct{}

func (s *fakeStream) Recv() (normalization.StreamEvent, error) { return normalization.StreamEvent{}, io.EOF }
func (s *fakeStream) Close() error                            { return nil }

func floatPtr(v float64) *float64 { return &v }

func testCoordinator(t *testing.T, cfg *config.Config, store *storage.Store) (*Coordinator, *fakeAdapter) {
	t.Helper()
	reg := provider.NewRegistry()
	ad := &fakeAdapter{}
	reg.Register(ad)
	return &Coordinator{Registry: reg, Store: store, Cfg: cfg, reserved: map[string]*spendReservation{}}, ad
}

func planAttempts(attempts ...router.Attempt) *router.Plan {
	for i := range attempts {
		attempts[i].Config.Type = "fake"
	}
	return &router.Plan{PublicModel: "alias", RouteName: "r", Attempts: attempts}
}

func reqWith() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{Messages: []normalization.Message{{
		Role:    "user",
		Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hello"}},
	}}}
}

// asNormErr extracts the normalized error from the coordinator's returned error.
func normErrOf(err error) *normalization.Error {
	if err == nil {
		return nil
	}
	if ne, ok := err.(*normalization.Error); ok {
		return ne
	}
	return nil
}

// --- Per-route pricing gate (unpriced rejection) ---

func TestUnpricedDirectPortableRejected(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-unknown"})

	_, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false})
	if err == nil || normErrOf(err).HTTPStatus != 402 || normErrOf(err).Code != normalization.ErrUnpriced {
		t.Fatalf("expected 402 unpriced_route, got %+v", err)
	}
	if normErrOf(err).Retryable {
		t.Fatalf("unpriced_route must be non-retryable")
	}
	if ad.calls != 0 {
		t.Fatalf("provider must not be called after unpriced rejection, got %d calls", ad.calls)
	}
}

func TestUnpricedPublicHostingRejected(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, _ := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", DailyEstimatedCostUSD: floatPtr(1.0)} // not portable
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-unknown"})
	_, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: true})
	if err == nil || normErrOf(err).HTTPStatus != 402 || normErrOf(err).Code != normalization.ErrUnpriced {
		t.Fatalf("public hosting must reject unpriced, got %+v", err)
	}
}

func TestPricedDirectProceeds(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-4o"})
	res, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false})
	if err != nil {
		t.Fatalf("priced route should proceed, got %v", err)
	}
	if res == nil || ad.calls != 1 {
		t.Fatalf("expected exactly one provider call, got %d", ad.calls)
	}
}

func TestUnpricedFallbackTargetRejected(t *testing.T) {
	// Priced first target but unpriced fallback must reject before any call.
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(
		router.Attempt{ProviderID: "openai", Model: "gpt-4o"},
		router.Attempt{ProviderID: "openai", Model: "gpt-unknown"},
	)
	_, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false})
	if err == nil || normErrOf(err).HTTPStatus != 402 {
		t.Fatalf("unpriced fallback must reject, got %+v", err)
	}
	if ad.calls != 0 {
		t.Fatalf("no provider call allowed when a fallback target is unpriced, got %d", ad.calls)
	}
}

func TestAllFallbackTargetsPricedProceeds(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{
		"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15},
		"openai/gpt-4o-mini": {InputUSDPerMillion: 1, OutputUSDPerMillion: 3},
	}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(
		router.Attempt{ProviderID: "openai", Model: "gpt-4o"},
		router.Attempt{ProviderID: "openai", Model: "gpt-4o-mini"},
	)
	if _, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false}); err != nil {
		t.Fatalf("all priced fallback should proceed, got %v", err)
	}
	if ad.calls != 1 {
		t.Fatalf("expected first attempt call, got %d", ad.calls)
	}
}

func TestSmartPlanUnpricedAttemptRejected(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, _ := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	// Simulate a smart-selected plan with one unpriced attempt among priced ones.
	plan := planAttempts(
		router.Attempt{ProviderID: "openai", Model: "gpt-4o"},
		router.Attempt{ProviderID: "anthropic", Model: "claude-unknown"},
	)
	_, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false})
	if err == nil || normErrOf(err).HTTPStatus != 402 {
		t.Fatalf("smart plan with unpriced attempt must reject, got %+v", err)
	}
}

func TestSmartPlanAllPricedProceeds(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{
		"openai/gpt-4o":   {InputUSDPerMillion: 5, OutputUSDPerMillion: 15},
		"anthropic/claude-x": {InputUSDPerMillion: 8, OutputUSDPerMillion: 24},
	}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(
		router.Attempt{ProviderID: "openai", Model: "gpt-4o"},
		router.Attempt{ProviderID: "anthropic", Model: "claude-x"},
	)
	if _, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false}); err != nil {
		t.Fatalf("all priced smart plan should proceed, got %v", err)
	}
	if ad.calls != 1 {
		t.Fatalf("expected one call, got %d", ad.calls)
	}
}

func TestLocalModelZeroPriceProceeds(t *testing.T) {
	// Explicit zero-rate entry is valid and must not be treated as unpriced.
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"local/llama": {InputUSDPerMillion: 0, OutputUSDPerMillion: 0}}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(router.Attempt{ProviderID: "local", Model: "llama"})
	if _, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false}); err != nil {
		t.Fatalf("explicit zero-price local model must proceed, got %v", err)
	}
	if ad.calls != 1 {
		t.Fatalf("expected one call, got %d", ad.calls)
	}
}

func TestNoBudgetNonPortableLocalProceeds(t *testing.T) {
	// No cost budget and not portable: documented fail-open policy; even an
	// unpriced route proceeds.
	cfg := &config.Config{} // no pricing at all
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1"} // no DailyEstimatedCostUSD, not portable
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-unknown"})
	if _, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false}); err != nil {
		t.Fatalf("no-budget local key must proceed, got %v", err)
	}
	if ad.calls != 1 {
		t.Fatalf("expected one call, got %d", ad.calls)
	}
}

// --- Reservation / daily budget ---

func TestConcurrentReservationsCannotExceedBudget(t *testing.T) {
	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, _ := testCoordinator(t, cfg, store)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-4o"})

	worst, _ := cfg.ComputeCost("openai", "gpt-4o", estimateInputTokens(reqWith()), maxOutputTokens(reqWith()))
	budget := worst * 1.5 // allows one, rejects the second concurrent
	key.DailyEstimatedCostUSD = &budget

	resv1, err1 := c.admit(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false})
	if err1 != nil {
		t.Fatalf("first concurrent request should be admitted, got %v", err1)
	}
	// Second concurrent request must be rejected (worst-case reservation sum exceeds budget).
	if _, err2 := c.admit(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false}); err2 == nil || normErrOf(err2).HTTPStatus != 429 {
		t.Fatalf("second concurrent request should be rejected (429), got %+v", err2)
	}
	c.releaseSpend(resv1)
}

// --- Streaming rejected before any provider call (consistency with Execute) ---

func TestUnpricedStreamingRejected(t *testing.T) {
	cfg := &config.Config{Pricing: map[string]config.PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}
	c, ad := testCoordinator(t, cfg, nil)
	key := &storage.ClientKey{ID: "k1", Portable: true, DailyEstimatedCostUSD: floatPtr(1.0)}
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-unknown"})
	_, err := c.ExecuteStream(context.Background(), reqWith(), plan, PolicyContext{ClientKey: key, PublicHosting: false})
	if err == nil || normErrOf(err).HTTPStatus != 402 || normErrOf(err).Code != normalization.ErrUnpriced {
		t.Fatalf("streaming unpriced must reject with 402, got %+v", err)
	}
	if ad.calls != 0 {
		t.Fatalf("no stream may open after unpriced rejection, got %d calls", ad.calls)
	}
}

// --- Playground policy: no client key => no spend enforcement ---

func TestPlaygroundNilKeyProceedsUnpriced(t *testing.T) {
	cfg := &config.Config{} // no pricing; model is unpriced, but playground has no client key
	c, ad := testCoordinator(t, cfg, nil)
	// Playground passes ClientKey: nil.
	plan := planAttempts(router.Attempt{ProviderID: "openai", Model: "gpt-unknown"})
	if _, err := c.Execute(context.Background(), reqWith(), plan, PolicyContext{ClientKey: nil, PublicHosting: false}); err != nil {
		t.Fatalf("playground without client key must proceed even if unpriced, got %v", err)
	}
	if ad.calls != 1 {
		t.Fatalf("expected one call, got %d", ad.calls)
	}
}
