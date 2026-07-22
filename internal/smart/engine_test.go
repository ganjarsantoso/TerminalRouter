package smart

import (
	"testing"

	"github.com/termrouter/termrouter/internal/normalization"
)

func testEngine() *Engine {
	user := map[string]ModelProfile{
		"local/qwen-coder": {
			ID: "local/qwen-coder", Source: SourceUser,
			ProviderID: "local", ModelID: "qwen-coder",
			Capabilities: map[string]float64{
				CapGeneral: 6, CapCoding: 10, CapReasoning: 8, CapAnalysis: 6, CapToolUse: 8,
			},
			Properties: ModelProperties{
				Tools: boolPtr(true), Vision: boolPtr(false),
				ContextWindow: 32768, CostTier: 1, LatencyTier: 1, Privacy: PrivacyLocal,
			},
		},
		"deepseek/deepseek-chat": {
			ID: "deepseek/deepseek-chat", Source: SourceUser,
			ProviderID: "deepseek", ModelID: "deepseek-chat",
			Capabilities: map[string]float64{
				CapGeneral: 8, CapCoding: 10, CapReasoning: 8, CapAnalysis: 8, CapToolUse: 8,
			},
			Properties: ModelProperties{
				Tools: boolPtr(true), Vision: boolPtr(false),
				ContextWindow: 64000, CostTier: 1, LatencyTier: 2, Privacy: PrivacyCloud,
			},
		},
		"anthropic-main/claude-sonnet": {
			ID: "anthropic-main/claude-sonnet", Source: SourceUser,
			ProviderID: "anthropic-main", ModelID: "claude-sonnet",
			Capabilities: map[string]float64{
				CapGeneral: 10, CapCoding: 10, CapReasoning: 10, CapAnalysis: 10, CapToolUse: 10,
			},
			Properties: ModelProperties{
				Tools: boolPtr(true), Vision: boolPtr(true),
				ContextWindow: 200000, CostTier: 4, LatencyTier: 3, Privacy: PrivacyCloud,
			},
		},
		"openai-main/reasoning-model": {
			ID: "openai-main/reasoning-model", Source: SourceUser,
			ProviderID: "openai-main", ModelID: "reasoning-model",
			Capabilities: map[string]float64{
				CapGeneral: 8, CapCoding: 10, CapReasoning: 10, CapAnalysis: 10, CapToolUse: 4,
			},
			Properties: ModelProperties{
				Tools: boolPtr(false), Vision: boolPtr(true),
				ContextWindow: 200000, CostTier: 5, LatencyTier: 5, Privacy: PrivacyCloud,
			},
		},
	}
	return &Engine{
		Profiles: NewProfileStore(user, true),
		Providers: map[string]ProviderState{
			"local":          {Enabled: true, HasCredential: true},
			"deepseek":       {Enabled: true, HasCredential: true},
			"anthropic-main": {Enabled: true, HasCredential: true},
			"openai-main":    {Enabled: true, HasCredential: true},
		},
		Affinity: NewMemoryAffinity(),
	}
}

func sampleRoute() RouteConfig {
	return RouteConfig{
		RouteID: "intelligent",
		Mode:    ModeLive,
		Policy:  PolicyBalanced,
		Candidates: []Candidate{
			{Provider: "local", Model: "qwen-coder", Order: 0},
			{Provider: "deepseek", Model: "deepseek-chat", Order: 1},
			{Provider: "anthropic-main", Model: "claude-sonnet", Order: 2},
			{Provider: "openai-main", Model: "reasoning-model", Order: 3},
		},
		DefaultProvider:     "anthropic-main",
		DefaultModel:        "claude-sonnet",
		StrictProfiles:      true,
		ConfidenceThreshold: 0.80,
		MinimumTaskMatch:    0.60,
	}
}

func TestSelectCodingDebugPrefersStrongCoder(t *testing.T) {
	eng := testEngine()
	req := &normalization.NormalizedRequest{
		ID: "req1",
		Messages: []normalization.Message{{
			Role: normalization.RoleUser,
			Content: []normalization.ContentBlock{{
				Type: normalization.ContentText,
				Text: "Find the concurrency bug in this Go worker pool.\n```go\nfunc (p *Pool) Submit() {\n  go func() { p.m[k]=v }()\n}\n```",
			}},
		}},
	}
	d, err := eng.Select(req, sampleRoute(), Override{})
	if err != nil {
		t.Fatal(err)
	}
	// Should not pick expensive reasoning model for balanced coding
	if d.SelectedProvider == "openai-main" {
		t.Fatalf("unexpected expensive pick: %s score=%v", d.SelectedKey(), d.SelectionScore)
	}
	// coding-capable selection
	if d.SelectedProvider != "deepseek" && d.SelectedProvider != "local" && d.SelectedProvider != "anthropic-main" {
		t.Fatalf("selected %s", d.SelectedKey())
	}
	// deepseek or local should score well for coding under balanced
	foundCoding := false
	for _, ev := range d.Evaluations {
		if ev.Eligible && (ev.Provider == "deepseek" || ev.Provider == "local") {
			foundCoding = true
		}
	}
	if !foundCoding {
		t.Fatal("expected coding candidates eligible")
	}
}

func TestEconomyCostCeiling(t *testing.T) {
	eng := testEngine()
	rc := sampleRoute()
	rc.Policy = PolicyEconomy
	req := &normalization.NormalizedRequest{
		ID: "req2",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hello"}},
		}},
	}
	d, err := eng.Select(req, rc, Override{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range d.Evaluations {
		if ev.Provider == "openai-main" && ev.Eligible {
			t.Fatal("economy should reject cost tier 5 openai-main")
		}
		if ev.Provider == "anthropic-main" && ev.Eligible {
			// cost tier 4 > ceiling 3
			t.Fatal("economy should reject anthropic cost tier 4")
		}
	}
	if d.SelectedProvider == "openai-main" || d.SelectedProvider == "anthropic-main" {
		t.Fatalf("economy selected expensive model %s", d.SelectedKey())
	}
}

func TestToolsHardConstraint(t *testing.T) {
	eng := testEngine()
	req := &normalization.NormalizedRequest{
		ID: "req3",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "Use tools to fetch weather"}},
		}},
		Tools:      []normalization.Tool{{Name: "weather"}},
		ToolChoice: &normalization.ToolChoice{Type: "required"},
	}
	d, err := eng.Select(req, sampleRoute(), Override{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range d.Evaluations {
		if ev.Provider == "openai-main" && ev.Eligible {
			t.Fatal("reasoning-model without tools must be rejected")
		}
	}
	if d.SelectedProvider == "openai-main" {
		t.Fatal("must not select tools-unsupported model")
	}
}

func TestPrivatePolicy(t *testing.T) {
	eng := testEngine()
	rc := sampleRoute()
	rc.Policy = PolicyPrivate
	req := &normalization.NormalizedRequest{
		ID: "req4",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "rewrite this sentence politely"}},
		}},
	}
	d, err := eng.Select(req, rc, Override{})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedProvider != "local" {
		t.Fatalf("private policy should select local, got %s", d.SelectedKey())
	}
}

func TestShadowDoesNotError(t *testing.T) {
	eng := testEngine()
	rc := sampleRoute()
	rc.Mode = ModeShadow
	req := &normalization.NormalizedRequest{
		ID: "req5",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "sum 2+2"}},
		}},
	}
	d, err := eng.Select(req, rc, Override{})
	if err != nil {
		t.Fatal(err)
	}
	if d.ShadowRecommendation == "" {
		t.Fatal("expected shadow recommendation")
	}
}

func TestSessionAffinity(t *testing.T) {
	eng := testEngine()
	rc := sampleRoute()
	rc.SessionAffinity = true
	rc.Mode = ModeLive
	req := &normalization.NormalizedRequest{
		ID: "req6",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "```go\nfunc main(){}\n``` debug this panic"}},
		}},
	}
	d1, err := eng.Select(req, rc, Override{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	req2 := &normalization.NormalizedRequest{
		ID: "req7",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "now check this function"}},
		}},
	}
	d2, err := eng.Select(req2, rc, Override{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !d2.SessionAffinity.Hit {
		t.Fatalf("expected affinity hit, got %+v selected=%s", d2.SessionAffinity, d2.SelectedKey())
	}
	if d2.SelectedProvider != d1.SelectedProvider || d2.SelectedModel != d1.SelectedModel {
		t.Fatalf("affinity pin mismatch %s vs %s", d1.SelectedKey(), d2.SelectedKey())
	}
}

func TestDeterministicSelection(t *testing.T) {
	eng := testEngine()
	req := &normalization.NormalizedRequest{
		ID: "req8",
		Messages: []normalization.Message{{
			Role:    normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "Write a Python fibonacci function"}},
		}},
	}
	d1, err := eng.Select(req, sampleRoute(), Override{})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := eng.Select(req, sampleRoute(), Override{})
	if err != nil {
		t.Fatal(err)
	}
	if d1.SelectedKey() != d2.SelectedKey() {
		t.Fatalf("non-deterministic selection %s vs %s", d1.SelectedKey(), d2.SelectedKey())
	}
}

func TestMinimumTaskMatchFloor(t *testing.T) {
	eng := testEngine()
	// tiny weak profile
	eng.Profiles.User["local/small"] = ModelProfile{
		ID: "local/small", ProviderID: "local", ModelID: "small", Source: SourceUser,
		Capabilities: map[string]float64{CapGeneral: 1, CapCoding: 1, CapReasoning: 1},
		Properties:   ModelProperties{Tools: boolPtr(false), CostTier: 1, LatencyTier: 1, Privacy: PrivacyLocal, ContextWindow: 4096},
	}
	rc := RouteConfig{
		RouteID: "t", Mode: ModeLive, Policy: PolicyEconomy,
		Candidates: []Candidate{
			{Provider: "local", Model: "small", Order: 0},
			{Provider: "deepseek", Model: "deepseek-chat", Order: 1},
		},
		DefaultProvider: "deepseek", DefaultModel: "deepseek-chat",
		StrictProfiles: true, MinimumTaskMatch: 0.60,
	}
	req := &normalization.NormalizedRequest{
		ID: "req9",
		Messages: []normalization.Message{{
			Role: normalization.RoleUser,
			Content: []normalization.ContentBlock{{
				Type: normalization.ContentText,
				Text: "Find the race condition in this concurrent Go code:\n```go\nvar m map[int]int\ngo func(){ m[1]=2 }()\n```",
			}},
		}},
	}
	d, err := eng.Select(req, rc, Override{})
	if err != nil {
		t.Fatal(err)
	}
	if d.SelectedModel == "small" {
		t.Fatal("weak model below floor should not win complex coding task")
	}
}
