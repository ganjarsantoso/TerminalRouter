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
