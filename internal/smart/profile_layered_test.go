package smart

import (
	"testing"

	"github.com/termrouter/termrouter/internal/config"
)

func newLayeredStore(t *testing.T, cfg *config.Config) *ProfileStore {
	t.Helper()
	return NewProfileStoreFromConfig(cfg, true)
}

func layeredConfig() *config.Config {
	cfg := config.Default()
	cfg.ModelProfiles = map[string]config.ModelProfileConfig{
		"provider/model": {
			ExternalBaseline: &config.ProfileBaseline{
				Version:      "registry-2026.07",
				Capabilities: map[string]float64{CapCoding: 8.7, CapGeneral: 7.5},
				Confidence:   map[string]float64{CapCoding: 0.86},
			},
			AssessmentBaseline: &config.ProfileBaseline{
				Version:      "assessment-v2",
				Capabilities: map[string]float64{CapToolUse: 6.0, CapCoding: 8.0},
				Confidence:   map[string]float64{CapToolUse: 0.92},
			},
			UserOverrides: &config.ProfileBaseline{
				Version:      "user",
				Capabilities: map[string]float64{CapCoding: 9.0},
			},
		},
	}
	return cfg
}

func TestLayeredPerFieldPrecedence(t *testing.T) {
	ps := newLayeredStore(t, layeredConfig())
	res := ps.ResolveDetailed("provider", "model", "provider/model")

	// user override wins for coding
	if f, ok := res.Capabilities[CapCoding]; !ok || f.Value != 9.0 || f.Source != SourceUser {
		t.Fatalf("coding: %+v", res.Capabilities[CapCoding])
	}
	// assessment wins for tool_use (user didn't set it)
	if f, ok := res.Capabilities[CapToolUse]; !ok || f.Value != 6.0 || f.Source != SourceSelfAssess {
		t.Fatalf("tool_use: %+v", res.Capabilities[CapToolUse])
	}
	// external wins for general (only external sets it)
	if f, ok := res.Capabilities[CapGeneral]; !ok || f.Value != 7.5 || f.Source != SourceExternal {
		t.Fatalf("general: %+v", res.Capabilities[CapGeneral])
	}
}

func TestExternalCannotOverwriteUserOverride(t *testing.T) {
	ps := newLayeredStore(t, layeredConfig())
	p, _ := ps.Resolve("provider", "model", "provider/model")
	if p.Cap(CapCoding) != 9.0 {
		t.Fatalf("external import must not overwrite user coding override: got %g", p.Cap(CapCoding))
	}
}

func TestAssessmentCannotOverwriteUserOverride(t *testing.T) {
	ps := newLayeredStore(t, layeredConfig())
	p, _ := ps.Resolve("provider", "model", "provider/model")
	// assessment sets tool_use but NOT a user value; ensure user has no tool_use
	// override. Here we assert assessment value is reported and not clobbered by
	// a hypothetical user tool_use override: simulate via store.
	ps.User["provider/model"] = ModelProfile{
		ID: "provider/model", Source: SourceUser,
		Capabilities: map[string]float64{CapToolUse: 9.5},
	}
	p, _ = ps.Resolve("provider", "model", "provider/model")
	if p.Cap(CapToolUse) != 9.5 {
		t.Fatalf("user tool_use override must outrank assessment: got %g", p.Cap(CapToolUse))
	}
}

func TestAssessmentOutranksExternal(t *testing.T) {
	cfg := config.Default()
	cfg.ModelProfiles = map[string]config.ModelProfileConfig{
		"p/m": {
			ExternalBaseline:   &config.ProfileBaseline{Capabilities: map[string]float64{CapCoding: 8.7}},
			AssessmentBaseline: &config.ProfileBaseline{Capabilities: map[string]float64{CapCoding: 8.0}},
		},
	}
	ps := newLayeredStore(t, cfg)
	p, _ := ps.Resolve("p", "m", "p/m")
	if p.Cap(CapCoding) != 8.0 {
		t.Fatalf("assessment should outrank external: got %g", p.Cap(CapCoding))
	}
}

func TestExternalOutranksBuiltin(t *testing.T) {
	cfg := config.Default()
	cfg.ModelProfiles = map[string]config.ModelProfileConfig{
		"p/m": {ExternalBaseline: &config.ProfileBaseline{Capabilities: map[string]float64{CapCoding: 8.7}}},
	}
	ps := newLayeredStore(t, cfg)
	p, found := ps.Resolve("p", "m", "p/m")
	if !found || p.Cap(CapCoding) != 8.7 || p.Source != SourceExternal {
		t.Fatalf("external should be found and applied: found=%v src=%s val=%g", found, p.Source, p.Cap(CapCoding))
	}
}

func TestResetUserOverrideRevealsLower(t *testing.T) {
	ps := newLayeredStore(t, layeredConfig())
	res := ps.ResolveDetailed("provider", "model", "provider/model")
	if res.Capabilities[CapCoding].Value != 9.0 {
		t.Fatalf("pre-reset coding should be user 9.0, got %g", res.Capabilities[CapCoding].Value)
	}
	// Drop the user override layer.
	delete(ps.User, "provider/model")
	res = ps.ResolveDetailed("provider", "model", "provider/model")
	if res.Capabilities[CapCoding].Value != 8.0 || res.Capabilities[CapCoding].Source != SourceSelfAssess {
		t.Fatalf("after reset coding should reveal assessment 8.0: %+v", res.Capabilities[CapCoding])
	}
	// Drop assessment too -> external.
	delete(ps.Assessments, "provider/model")
	res = ps.ResolveDetailed("provider", "model", "provider/model")
	if res.Capabilities[CapCoding].Value != 8.7 || res.Capabilities[CapCoding].Source != SourceExternal {
		t.Fatalf("after reset coding should reveal external 8.7: %+v", res.Capabilities[CapCoding])
	}
}

func TestLegacyFlatProfileMigration(t *testing.T) {
	cfg := config.Default()
	cfg.ModelProfiles = map[string]config.ModelProfileConfig{
		"p/m": {
			Source:       SourceUser,
			Version:      "legacy",
			Capabilities: map[string]float64{CapCoding: 9.0},
			Properties:   config.ModelPropertiesConfig{Privacy: "cloud"},
		},
	}
	cfg.NormalizeProfiles()
	mp := cfg.ModelProfiles["p/m"]
	if mp.UserOverrides == nil || mp.UserOverrides.Capabilities[CapCoding] != 9.0 {
		t.Fatalf("legacy user profile not migrated to UserOverrides: %+v", mp)
	}
	if mp.UserOverrides.Properties == nil || mp.UserOverrides.Properties.Privacy != "cloud" {
		t.Fatalf("legacy properties not migrated: %+v", mp.UserOverrides)
	}
	// Idempotent: running again must not duplicate or break.
	cfg.NormalizeProfiles()
	mp2 := cfg.ModelProfiles["p/m"]
	if mp2.UserOverrides == nil || mp2.UserOverrides.Capabilities[CapCoding] != 9.0 {
		t.Fatalf("migration not idempotent: %+v", mp2)
	}
	// Legacy fields cleared so they aren't re-serialized.
	if mp.Source != "" || len(mp.Capabilities) != 0 {
		t.Fatalf("legacy fields should be cleared: %+v", mp)
	}

	// self-assessment source migrates to assessment baseline.
	cfg2 := config.Default()
	cfg2.ModelProfiles = map[string]config.ModelProfileConfig{
		"p/m2": {Source: SourceSelfAssess, Capabilities: map[string]float64{CapReasoning: 7.0}},
	}
	cfg2.NormalizeProfiles()
	if cfg2.ModelProfiles["p/m2"].AssessmentBaseline == nil ||
		cfg2.ModelProfiles["p/m2"].AssessmentBaseline.Capabilities[CapReasoning] != 7.0 {
		t.Fatalf("self-assessment not migrated: %+v", cfg2.ModelProfiles["p/m2"])
	}

	// external-consensus source migrates to external baseline.
	cfg3 := config.Default()
	cfg3.ModelProfiles = map[string]config.ModelProfileConfig{
		"p/m3": {Source: SourceExternal, Capabilities: map[string]float64{CapGeneral: 8.0}},
	}
	cfg3.NormalizeProfiles()
	if cfg3.ModelProfiles["p/m3"].ExternalBaseline == nil ||
		cfg3.ModelProfiles["p/m3"].ExternalBaseline.Capabilities[CapGeneral] != 8.0 {
		t.Fatalf("external not migrated: %+v", cfg3.ModelProfiles["p/m3"])
	}
}

func TestEffectiveProfileConsumedBySmartRoutes(t *testing.T) {
	// Layer merge must yield a single effective ModelProfile usable by scoring.
	ps := newLayeredStore(t, layeredConfig())
	p, found := ps.Resolve("provider", "model", "provider/model")
	if !found {
		t.Fatal("expected found")
	}
	// effective capabilities combine all three layers.
	if p.Cap(CapCoding) != 9.0 || p.Cap(CapToolUse) != 6.0 || p.Cap(CapGeneral) != 7.5 {
		t.Fatalf("effective merge wrong: coding=%g tool=%g gen=%g",
			p.Cap(CapCoding), p.Cap(CapToolUse), p.Cap(CapGeneral))
	}
}

func TestAssessmentApplyCannotOverwriteUserOverride(t *testing.T) {
	// Simulate the CLI modelAssessApply flow: write assessment values into
	// AssessmentBaseline, then prove user overrides still win on resolution.
	cfg := config.Default()
	cfg.ModelProfiles = map[string]config.ModelProfileConfig{
		"p/m": {
			ExternalBaseline: &config.ProfileBaseline{
				Capabilities: map[string]float64{CapCoding: 7.0},
			},
			AssessmentBaseline: &config.ProfileBaseline{
				Capabilities: map[string]float64{CapCoding: 8.0},
			},
			UserOverrides: &config.ProfileBaseline{
				Capabilities: map[string]float64{CapCoding: 9.5},
			},
		},
	}
	ps := NewProfileStoreFromConfig(cfg, true)

	// Phase 1: before applying new assessment, user override is effective.
	res := ps.ResolveDetailed("p", "m", "p/m")
	if f := res.Capabilities[CapCoding]; f.Value != 9.5 || f.Source != SourceUser {
		t.Fatalf("pre-apply: expected user=9.5, got %+v", f)
	}

	// Phase 2: simulate modelAssessApply writing new assessment values
	// (e.g. from a completed assessment run) into AssessmentBaseline.
	// This mirrors the CLI code in model_profile.go modelAssessApply().
	mp := cfg.ModelProfiles["p/m"]
	bl := mp.AssessmentBaseline
	bl.Version = "assessment-v3"
	for k, v := range map[string]float64{CapCoding: 7.5, CapToolUse: 6.5} {
		bl.Capabilities[k] = v
	}
	cfg.ModelProfiles["p/m"] = mp

	// Rebuild store from the modified config.
	ps = NewProfileStoreFromConfig(cfg, true)

	// Phase 3: user override must still win for CapCoding.
	res = ps.ResolveDetailed("p", "m", "p/m")
	if f := res.Capabilities[CapCoding]; f.Value != 9.5 || f.Source != SourceUser {
		t.Fatalf("post-apply: expected user=9.5, got %+v", f)
	}
	// CapToolUse was not user-overridden; assessment value is used.
	if f := res.Capabilities[CapToolUse]; f.Value != 6.5 || f.Source != SourceSelfAssess {
		t.Fatalf("post-apply: expected assessment=6.5 for tool_use, got %+v", f)
	}
	// CapGeneral was not set by assessment or user; external baseline surfaces.
	if f := res.Capabilities[CapGeneral]; f.Value != 0 {
		t.Fatalf("expected general to be absent/zero, got %+v", f)
	}
}
