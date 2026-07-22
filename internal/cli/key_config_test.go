package cli

import (
	"errors"
	"strings"
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

func TestValidateEffectiveClientKeyPolicy_PublicCannotClearAliases(t *testing.T) {
	// Existing portable key in public mode: clearing aliases should fail.
	key := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        nil, // cleared
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(key, ctx)
	if err == nil {
		t.Fatal("expected error when portable key has no aliases in public mode")
	}
	var pe *PortableKeyPolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PortableKeyPolicyError, got %T: %v", err, err)
	}
}

func TestValidateEffectiveClientKeyPolicy_PublicCannotClearBodyLimit(t *testing.T) {
	key := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        nil, // cleared
		DailyEstimatedCostUSD: ptr(10.0),
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(key, ctx)
	if err == nil {
		t.Fatal("expected error when portable key has no body limit in public mode")
	}
	var pe *PortableKeyPolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PortableKeyPolicyError, got %T: %v", err, err)
	}
}

func TestValidateEffectiveClientKeyPolicy_PublicCannotClearCostBudget(t *testing.T) {
	key := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: nil, // cleared
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(key, ctx)
	if err == nil {
		t.Fatal("expected error when portable key has no cost budget in public mode")
	}
	var pe *PortableKeyPolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PortableKeyPolicyError, got %T: %v", err, err)
	}
}

func TestValidateEffectiveClientKeyPolicy_ChangingUnrelatedRPM_Succeeds(t *testing.T) {
	// Changing RPM on a fully valid portable key is fine.
	key := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
		RateLimitRPM:          ptr(30),
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	if err := ValidateEffectiveClientKeyPolicy(key, ctx); err != nil {
		t.Fatalf("expected valid portable key with RPM change to pass, got %v", err)
	}
}

func TestValidateEffectiveClientKeyPolicy_NonPortableAlwaysPasses(t *testing.T) {
	key := storage.ClientKey{Portable: false}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	if err := ValidateEffectiveClientKeyPolicy(key, ctx); err != nil {
		t.Fatalf("non-portable key should always pass, got %v", err)
	}
}

func TestValidateEffectiveClientKeyPolicy_UnsafeOverrideAllowsUnrestricted(t *testing.T) {
	key := storage.ClientKey{
		Portable: true,
		// No controls set
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: true}
	if err := ValidateEffectiveClientKeyPolicy(key, ctx); err != nil {
		t.Fatalf("unsafe override should permit unrestricted portable key, got %v", err)
	}
}

func TestValidateEffectiveClientKeyPolicy_LocalModeAllowsUnrestricted(t *testing.T) {
	key := storage.ClientKey{
		Portable: true,
		// No controls set
	}
	ctx := KeyPolicyValidationContext{PublicHosting: false, UnsafePortableOverride: false}
	if err := ValidateEffectiveClientKeyPolicy(key, ctx); err != nil {
		t.Fatalf("local mode should permit unrestricted portable key, got %v", err)
	}
}

func TestValidateEffectiveClientKeyPolicy_StableErrorCode(t *testing.T) {
	key := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        nil,
		MaxRequestBody:        nil,
		DailyEstimatedCostUSD: nil,
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(key, ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	errStr := err.Error()
	if !strings.HasPrefix(errStr, "portable_key_policy_unsafe:") {
		t.Fatalf("expected error to start with 'portable_key_policy_unsafe:', got %q", errStr)
	}
}

func TestValidateEffectiveClientKeyPolicy_MultiFieldPatchFails(t *testing.T) {
	// Clearing multiple mandatory fields simultaneously still fails.
	key := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        nil, // cleared
		MaxRequestBody:        nil, // cleared
		DailyEstimatedCostUSD: nil, // cleared
	}
	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(key, ctx)
	if err == nil {
		t.Fatal("expected error when clearing all mandatory controls")
	}
	// Verify all three are reported in the error.
	var pe *PortableKeyPolicyError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *PortableKeyPolicyError, got %T", err)
	}
	if len(pe.Missing) != 3 {
		t.Fatalf("expected 3 missing controls, got %v", pe.Missing)
	}
}

// TestPortableKeySetPolicyFlow validates that keySetPolicy's validation flow
// works correctly: load -> copy -> apply -> validate -> persist. This is a
// white-box test simulating the validation step that keySetPolicy performs.
func TestPortableKeySetPolicyFlow_ClearingAliasesRejected(t *testing.T) {
	// Simulate: existing portable key with all controls, user clears aliases.
	existing := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	k := existing
	k.AllowedAliases = nil // user cleared aliases

	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(k, ctx)
	if err == nil {
		t.Fatal("clearing aliases on public portable key should fail validation")
	}

	// Original must be untouched.
	if len(existing.AllowedAliases) != 1 || existing.AllowedAliases[0] != "coding" {
		t.Fatal("original key should be unchanged after failed validation")
	}
}

func TestPortableKeySetPolicyFlow_ClearingBodyLimitRejected(t *testing.T) {
	existing := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	k := existing
	k.MaxRequestBody = nil

	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(k, ctx)
	if err == nil {
		t.Fatal("clearing body limit on public portable key should fail validation")
	}

	if existing.MaxRequestBody == nil || *existing.MaxRequestBody != 1<<20 {
		t.Fatal("original key should be unchanged after failed validation")
	}
}

func TestPortableKeySetPolicyFlow_ClearingCostBudgetRejected(t *testing.T) {
	existing := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	k := existing
	k.DailyEstimatedCostUSD = nil

	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: false}
	err := ValidateEffectiveClientKeyPolicy(k, ctx)
	if err == nil {
		t.Fatal("clearing cost budget on public portable key should fail validation")
	}

	if existing.DailyEstimatedCostUSD == nil || *existing.DailyEstimatedCostUSD != 10.0 {
		t.Fatal("original key should be unchanged after failed validation")
	}
}

func TestPortableKeySetPolicyFlow_UnsafeOverridePermitsMutation(t *testing.T) {
	existing := storage.ClientKey{
		Portable:              true,
		AllowedAliases:        []string{"coding"},
		MaxRequestBody:        ptr(int64(1 << 20)),
		DailyEstimatedCostUSD: ptr(10.0),
	}
	k := existing
	k.AllowedAliases = nil // clear aliases

	ctx := KeyPolicyValidationContext{PublicHosting: true, UnsafePortableOverride: true}
	if err := ValidateEffectiveClientKeyPolicy(k, ctx); err != nil {
		t.Fatalf("unsafe override should permit clearing aliases, got %v", err)
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
