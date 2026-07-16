package router

import (
	"testing"

	"github.com/termrouter/termrouter/internal/config"
)

func sampleCfg() *config.Config {
	cfg := config.Default()
	cfg.Providers["a"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: "http://a"}
	cfg.Providers["b"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: "http://b"}
	cfg.Routes["coding-route"] = config.RouteConfig{
		Strategy: "fallback",
		Targets: []config.TargetConfig{
			{Provider: "a", Model: "model-a"},
			{Provider: "b", Model: "model-b"},
		},
	}
	cfg.Aliases["coding"] = config.AliasConfig{Route: "coding-route"}
	cfg.Aliases["fast"] = config.AliasConfig{Provider: "a", Model: "tiny"}
	return cfg
}

func TestResolveAliasFallback(t *testing.T) {
	r := NewResolver(sampleCfg())
	plan, err := r.Resolve("coding", false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != "fallback" || len(plan.Attempts) != 2 {
		t.Fatalf("%+v", plan)
	}
	if plan.Attempts[0].Model != "model-a" {
		t.Fatal(plan.Attempts[0])
	}
}

func TestResolveDirectAlias(t *testing.T) {
	r := NewResolver(sampleCfg())
	plan, err := r.Resolve("fast", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Attempts) != 1 || plan.Attempts[0].Model != "tiny" {
		t.Fatalf("%+v", plan)
	}
}

func TestResolveDirectSyntax(t *testing.T) {
	r := NewResolver(sampleCfg())
	plan, err := r.Resolve("a/my-model", true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Attempts[0].ProviderID != "a" || plan.Attempts[0].Model != "my-model" {
		t.Fatalf("%+v", plan)
	}
	_, err = r.Resolve("a/my-model", false)
	if err == nil {
		t.Fatal("direct should be blocked")
	}
}

func TestUnknownModel(t *testing.T) {
	r := NewResolver(sampleCfg())
	_, err := r.Resolve("nope", false)
	if err == nil {
		t.Fatal("expected error")
	}
}
