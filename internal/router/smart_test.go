package router

import (
	"testing"

	"github.com/termrouter/termrouter/internal/config"
)

func TestResolveSmartRoute(t *testing.T) {
	cfg := config.Default()
	cfg.Providers["local"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: "http://l"}
	cfg.Providers["deepseek"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: "http://d"}
	cfg.Routes["intelligent"] = config.RouteConfig{
		Strategy: "smart",
		Candidates: []config.CandidateConfig{
			{Provider: "local", Model: "qwen-coder"},
			{Provider: "deepseek", Model: "deepseek-chat"},
		},
		Smart: &config.SmartConfig{Mode: "shadow", Policy: "balanced"},
	}
	cfg.Aliases["auto"] = config.AliasConfig{Route: "intelligent"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	r := NewResolver(cfg)
	plan, err := r.Resolve("auto", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "smart" {
		t.Fatalf("strategy=%s", plan.Strategy)
	}
	if plan.Smart == nil || plan.Smart.Mode != "shadow" {
		t.Fatalf("smart meta=%+v", plan.Smart)
	}
	if len(plan.Attempts) != 2 {
		t.Fatalf("attempts=%d", len(plan.Attempts))
	}
}

func TestDirectRoutesUnchanged(t *testing.T) {
	cfg := sampleCfg()
	r := NewResolver(cfg)
	plan, err := r.Resolve("coding", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "fallback" {
		t.Fatalf("strategy=%s", plan.Strategy)
	}
}
