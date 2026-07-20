package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultValidate(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := Default()
	cfg.Providers["mock"] = ProviderConfig{
		Type:          "openai-compatible",
		BaseURL:       "http://127.0.0.1:9",
		CredentialRef: "env://MOCK_KEY",
	}
	cfg.Routes["main"] = RouteConfig{
		Strategy: "fallback",
		Targets:  []TargetConfig{{Provider: "mock", Model: "m1"}},
	}
	cfg.Aliases["coding"] = AliasConfig{Route: "main"}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Aliases["coding"].Route != "main" {
		t.Fatalf("alias route = %q", loaded.Aliases["coding"].Route)
	}
	if loaded.Providers["mock"].BaseURL != "http://127.0.0.1:9" {
		t.Fatal("provider base url mismatch")
	}
}

func TestValidateRejectsUnknownRoute(t *testing.T) {
	cfg := Default()
	cfg.Aliases["x"] = AliasConfig{Route: "missing"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}

func TestPublicHostingRequiresLoopback(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		insecure bool
		wantErr  bool
	}{
		{"loopback ipv4", "127.0.0.1", false, false},
		{"loopback ipv6", "::1", false, false},
		{"localhost", "localhost", false, false},
		{"wildcard rejected", "0.0.0.0", false, true},
		{"wildcard rejected even with insecure_remote", "0.0.0.0", true, true},
		{"ipv6 wildcard rejected", "::", false, true},
		{"public addr rejected", "203.0.113.10", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default()
			cfg.PublicHosting.Enabled = true
			cfg.Server.AuthRequired = true
			cfg.Server.Host = tc.host
			cfg.Server.InsecureRemote = tc.insecure
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("host %q insecure=%v: expected error", tc.host, tc.insecure)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("host %q: unexpected error %v", tc.host, err)
			}
		})
	}
}

func TestExportSanitized(t *testing.T) {
	cfg := Default()
	cfg.Providers["p"] = ProviderConfig{
		Type: "openai", CredentialRef: "vault://secret-name",
	}
	san := cfg.ExportSanitized()
	if san.Providers["p"].CredentialRef != "vault://[redacted]" {
		t.Fatalf("got %q", san.Providers["p"].CredentialRef)
	}
}

func TestParseMaxRequestSize(t *testing.T) {
	n, err := ParseMaxRequestSize("20MiB")
	if err != nil || n != 20<<20 {
		t.Fatalf("got %d %v", n, err)
	}
}

func TestEnsureDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tr")
	p := ResolvePaths(root)
	if err := EnsureDirs(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.LogsDir); err != nil {
		t.Fatal(err)
	}
}

func TestComputeCostUsesPricingSource(t *testing.T) {
	cfg := &Config{
		Pricing: map[string]PriceConfig{
			"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15},
			"openai":        {InputUSDPerMillion: 1, OutputUSDPerMillion: 2},
		},
	}
	// Exact provider/model match.
	if cost, ok := cfg.ComputeCost("openai", "gpt-4o", 1_000_000, 1_000_000); !ok || cost != 20 {
		t.Fatalf("exact match: got %v ok=%v, want 20 true", cost, ok)
	}
	// Provider-only fallback.
	if cost, ok := cfg.ComputeCost("openai", "gpt-5", 1_000_000, 0); !ok || cost != 1 {
		t.Fatalf("provider fallback: got %v ok=%v, want 1 true", cost, ok)
	}
	// Unknown provider/model with no wildcard => unpriced.
	if _, ok := cfg.ComputeCost("anthropic", "claude-x", 1, 1); ok {
		t.Fatalf("unknown price should be unpriced (ok=false)")
	}
}

func TestLookupPricePrefersExact(t *testing.T) {
	cfg := &Config{
		Pricing: map[string]PriceConfig{
			"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15},
			"openai":        {InputUSDPerMillion: 1, OutputUSDPerMillion: 2},
		},
	}
	if p, ok := cfg.LookupPrice("openai", "gpt-4o"); !ok || p.InputUSDPerMillion != 5 || p.Source != "openai/gpt-4o" {
		t.Fatalf("exact match expected, got %+v ok=%v", p, ok)
	}
	if p, ok := cfg.LookupPrice("openai", "gpt-5"); !ok || p.InputUSDPerMillion != 1 || p.Source != "openai" {
		t.Fatalf("provider fallback expected, got %+v ok=%v", p, ok)
	}
	if _, ok := cfg.LookupPrice("anthropic", "claude-x"); ok {
		t.Fatalf("unknown price should be unpriced")
	}
}

func TestValidatePricing(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{"negative input", &Config{Pricing: map[string]PriceConfig{"p/m": {InputUSDPerMillion: -1}}}, true},
		{"negative output", &Config{Pricing: map[string]PriceConfig{"p/m": {OutputUSDPerMillion: -2}}}, true},
		{"unknown currency", &Config{Pricing: map[string]PriceConfig{"p/m": {InputUSDPerMillion: 1, Currency: "eur"}}}, true},
		{"explicit zero local price valid", &Config{Pricing: map[string]PriceConfig{"local/llama": {InputUSDPerMillion: 0, OutputUSDPerMillion: 0}}}, false},
		{"normal valid", &Config{Pricing: map[string]PriceConfig{"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15}}}, false},
		// Duplicate exact pricing entries are impossible in a Go map (last wins);
		// an explicit provider entry plus an exact entry is valid and resolved
		// by specificity, not an error.
		{"provider plus exact", &Config{Pricing: map[string]PriceConfig{
			"openai":        {InputUSDPerMillion: 1, OutputUSDPerMillion: 2},
			"openai/gpt-4o": {InputUSDPerMillion: 5, OutputUSDPerMillion: 15},
		}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.ValidatePricing()
			if tc.wantErr && err == nil {
				t.Fatalf("expected validation error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
