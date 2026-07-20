package cli

import (
	"testing"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/storage"
)

func ptr[T any](v T) *T { return &v }

func TestPortableMissingControls(t *testing.T) {
	// Fully restricted portable key: no missing controls.
	restricted := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	if m := portableMissingControls(restricted); len(m) != 0 {
		t.Fatalf("expected no missing controls, got %v", m)
	}

	// Unrestricted portable key: all three mandatory controls missing.
	unrestricted := storage.ClientKey{Portable: true}
	got := portableMissingControls(unrestricted)
	want := map[string]bool{"--alias": false, "--max-request-body": false, "--daily-cost-usd": false}
	if len(got) != 3 {
		t.Fatalf("expected 3 missing controls, got %v", got)
	}
	for _, c := range got {
		if _, ok := want[c]; !ok {
			t.Fatalf("unexpected missing control %q", c)
		}
		want[c] = true
	}
	for c, seen := range want {
		if !seen {
			t.Fatalf("expected missing control %q not reported", c)
		}
	}
}

func TestCheckPortableKeySafetyLocalMode(t *testing.T) {
	// Local-only mode: portable key without controls is permitted (no error).
	cfg := &config.Config{}
	k := storage.ClientKey{Portable: true}
	if err := checkPortableKeySafety(cfg, k, false); err != nil {
		t.Fatalf("local mode should permit unrestricted portable key, got %v", err)
	}
	// Non-portable key: never enforced.
	if err := checkPortableKeySafety(cfg, storage.ClientKey{Portable: false}, false); err != nil {
		t.Fatalf("non-portable key should never fail, got %v", err)
	}
}

func TestCheckPortableKeySafetyPublicMode(t *testing.T) {
	cfg := &config.Config{}
	cfg.PublicHosting.Enabled = true

	// Public mode without controls: rejected (fail-closed).
	unrestricted := storage.ClientKey{Portable: true}
	if err := checkPortableKeySafety(cfg, unrestricted, false); err == nil {
		t.Fatal("expected public-mode unrestricted portable key to be rejected")
	}

	// Public mode with all controls: allowed.
	restricted := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	if err := checkPortableKeySafety(cfg, restricted, false); err != nil {
		t.Fatalf("public-mode restricted portable key should be allowed, got %v", err)
	}

	// Public mode with --unsafe override: allowed even if unrestricted.
	if err := checkPortableKeySafety(cfg, unrestricted, true); err != nil {
		t.Fatalf("explicit --unsafe override should permit unrestricted portable key, got %v", err)
	}
}

func TestApplyKeyLimitFlagsExpires(t *testing.T) {
	var k storage.ClientKey
	// Valid RFC3339 timestamp accepted.
	if err := applyKeyLimitFlags(&k, 0, 0, 0, 0, 0, 0, 0, 0, "2030-01-01T00:00:00Z"); err != nil {
		t.Fatalf("valid RFC3339 expires should be accepted, got %v", err)
	}
	if k.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be set")
	}
	// Invalid timestamp rejected.
	if err := applyKeyLimitFlags(&k, 0, 0, 0, 0, 0, 0, 0, 0, "next tuesday"); err == nil {
		t.Fatal("expected invalid --expires to be rejected")
	}
}
