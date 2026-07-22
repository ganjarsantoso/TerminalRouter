package external

import "strings"

// ModelIdentity is a model referenced for evidence lookup. It is derived
// directly from the configured provider/model — there is no curated directory;
// any model can be searched live. It carries NO scores (those are fetched live).
//
// Beyond the basic id/name, it holds the canonical identity fields used for
// variant matching (§18): creator, family, version, release date, variant,
// reasoning effort, preview/stable status, quantization, context mode, endpoint
// provider, and harness. Evidence sourced from the web may carry a different
// published identity (e.g. "GPT-4o" vs the configured "openai/gpt-4o", or a
// thinking variant), and Match compares the canonical fields to decide how much
// an evidence record may contribute.
type ModelIdentity struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`

	// Canonical identity fields (§18). Empty fields are treated as "unknown"
	// and do not by themselves cause an incompatible match.
	Creator          string `json:"creator,omitempty"`
	Family           string `json:"family,omitempty"`
	Version          string `json:"version,omitempty"`
	ReleaseDate      string `json:"release_date,omitempty"`
	Variant          string `json:"variant,omitempty"`          // e.g. "base", "thinking", "instruct"
	ReasoningEffort  string `json:"reasoning_effort,omitempty"` // e.g. "standard", "high"
	PreviewStable    string `json:"preview_stable,omitempty"`   // "preview" | "stable"
	Quantization     string `json:"quantization,omitempty"`     // e.g. "q4", "fp16", "full"
	ContextMode      string `json:"context_mode,omitempty"`     // e.g. "standard", "long"
	EndpointProvider string `json:"endpoint_provider,omitempty"`
	Harness          string `json:"harness,omitempty"`
}

// identityFor builds a ModelIdentity directly from a provider/model pair.
// Any model is accepted; we only normalize the id for matching/caching.
func identityFor(providerID, modelID string) ModelIdentity {
	id := strings.TrimSpace(providerID) + "/" + strings.TrimSpace(modelID)
	return ModelIdentity{
		ID:       id,
		Name:     modelID,
		Provider: providerID,
		Model:    modelID,
		Creator:  providerID,
		Family:   modelID,
	}
}

// parsePublishedIdentity builds a ModelIdentity from a free-form model name as
// reported by an evidence source, extracting provider/org, family, version-
// related, preview/stable, reasoning-effort, quantization, and context-mode
// signals when present. It is used so the PUBLISHED identity is established
// independently of the configured (canonical) identity: never copy the
// configured identity into the evidence until the variant is independently
// confirmed, or variant matching (§18) would falsely report "exact". When the
// name is empty, it returns a zero identity, which Match treats as exact —
// preserving the legacy "unknown published identity" behaviour.
func parsePublishedIdentity(name string) ModelIdentity {
	name = strings.TrimSpace(name)
	if name == "" {
		return ModelIdentity{}
	}
	id := ModelIdentity{Name: name, Model: name}
	var model string
	if i := strings.Index(name, "/"); i >= 0 {
		org := name[:i]
		model = name[i+1:]
		id.Provider = org
		id.Creator = org
		id.Model = model
		id.ID = name
	} else {
		model = name
		id.ID = name
	}
	low := strings.ToLower(name)

	// Capture variant signals, then strip them from the family so the family
	// comparison (§18) matches on the base model, not on the variant suffix.
	switch {
	case strings.Contains(low, "preview"):
		id.PreviewStable = "preview"
	case strings.Contains(low, "stable"):
		id.PreviewStable = "stable"
	}
	switch {
	case strings.Contains(low, "thinking") || strings.Contains(low, "reason"):
		id.ReasoningEffort = "high"
	case strings.Contains(low, "base") || strings.Contains(low, "instruct"):
		id.ReasoningEffort = "base"
	}
	for _, q := range []string{"q4", "q8", "q2", "q2_k", "fp16", "fp32", "awq", "gptq", "int4", "int8"} {
		if strings.Contains(low, q) {
			id.Quantization = q
			break
		}
	}
	if strings.Contains(low, "long") {
		id.ContextMode = "long"
	}

	// Family is the model name with variant/quantization tokens removed so
	// "gpt-5-preview" and "gpt-5" share the family "gpt-5".
	id.Family = stripVariantTokens(model, []string{
		"preview", "stable", "thinking", "reasoning", "base", "instruct",
		"q4", "q8", "q2", "q2_k", "fp16", "fp32", "awq", "gptq", "int4", "int8",
	})
	return id
}

// stripVariantTokens removes whole-word variant/quantization tokens from a
// model name, rejoining the remaining tokens with "-".
func stripVariantTokens(s string, toks []string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '_' || r == '-' || r == '.'
	})
	out := parts[:0]
	for _, p := range parts {
		drop := false
		for _, t := range toks {
			if strings.EqualFold(p, t) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return s
	}
	return strings.Join(out, "-")
}

// MatchLevel classifies how closely a published evidence identity matches the
// configured (canonical) identity (§18).
type MatchLevel string

const (
	// MatchExact means the canonical identity fields agree; full eligible weight.
	MatchExact MatchLevel = "exact"
	// MatchStrongProbable means compatible variants differ (e.g. reasoning
	// effort, preview/stable, quantization, context mode); reduced weight and
	// mandatory review.
	MatchStrongProbable MatchLevel = "strong_probable"
	// MatchFamilyOnly means only the creator/family agrees (e.g. different
	// version); display only, excluded from scoring by default.
	MatchFamilyOnly MatchLevel = "family_only"
	// MatchIncompatible means the identities are not the same model (different
	// creator/family, or base-vs-thinking mismatch); excluded.
	MatchIncompatible MatchLevel = "incompatible"
)

// IdentityMatch describes the outcome of matching a published identity against
// the configured identity. It carries the level, the differing fields, and
// whether the evidence contributes to scoring — the data the UI (§18) needs.
type IdentityMatch struct {
	Level           MatchLevel `json:"level"`
	DifferingFields []string   `json:"differing_fields"`
	Contributes     bool       `json:"contributes"`
	MandatoryReview bool       `json:"mandatory_review"`
	Weight          float64    `json:"weight"`
}

// Match compares the configured identity (a) with a published identity (b) and
// returns how the evidence should be treated. When b is the zero value (no
// published identity available), it is treated as an exact match of a (the
// common case today, where the configured identity is the only identity we
// have), preserving existing behavior.
func (a ModelIdentity) Match(b ModelIdentity) IdentityMatch {
	if b.ID == "" && b.Creator == "" && b.Family == "" && b.Model == "" {
		return IdentityMatch{Level: MatchExact, Contributes: true, Weight: 1.0}
	}
	if a.ID != "" && b.ID != "" && a.ID == b.ID {
		return IdentityMatch{Level: MatchExact, Contributes: true, Weight: 1.0}
	}

	creatorA, creatorB := a.Creator, b.Creator
	if creatorA == "" {
		creatorA = a.Provider
	}
	if creatorB == "" {
		creatorB = b.Provider
	}
	familyA, familyB := a.Family, b.Family
	if familyA == "" {
		familyA = a.Model
	}
	if familyB == "" {
		familyB = b.Model
	}

	// Different creator/family => not the same model.
	if creatorA != "" && creatorB != "" && !strings.EqualFold(creatorA, creatorB) {
		return IdentityMatch{Level: MatchIncompatible, Contributes: false, Weight: 0}
	}
	if familyA != "" && familyB != "" && !strings.EqualFold(familyA, familyB) {
		return IdentityMatch{Level: MatchIncompatible, Contributes: false, Weight: 0}
	}

	// Same creator/family. Decide exact vs variant vs family-only.
	var diff []string
	eq := func(field, va, vb string) {
		if va == "" || vb == "" {
			return
		}
		if !strings.EqualFold(va, vb) {
			diff = append(diff, field)
		}
	}
	// Version/release-date and harness use field equality; a missing value on
	// either side is treated as "unknown" and does not by itself create a diff.
	eq("version", a.Version, b.Version)
	eq("release_date", a.ReleaseDate, b.ReleaseDate)
	eq("harness", a.Harness, b.Harness)

	// Variant fields: a specific variant asserted on the PUBLISHED side that the
	// configured side does NOT claim is a real difference (strong probable),
	// because the evidence may be about a more specific variant of the model
	// (e.g. a "preview" or "thinking" build). Without this, a configured base
	// model would falsely match "exact" against evidence for its preview
	// variant, defeating variant matching (§18). A variant claimed only by the
	// configured side with no statement from the evidence is trusted as exact.
	variantDiff := func(field, va, vb string) {
		if va != "" && vb != "" && !strings.EqualFold(va, vb) {
			diff = append(diff, field) // clear mismatch
			return
		}
		if va == "" && vb != "" {
			diff = append(diff, field) // published asserts a variant the configured model doesn't claim
		}
	}
	variantDiff("variant", a.Variant, b.Variant)
	variantDiff("reasoning_effort", a.ReasoningEffort, b.ReasoningEffort)
	variantDiff("preview_stable", a.PreviewStable, b.PreviewStable)
	variantDiff("quantization", a.Quantization, b.Quantization)
	variantDiff("context_mode", a.ContextMode, b.ContextMode)

	if len(diff) == 0 {
		return IdentityMatch{Level: MatchExact, Contributes: true, Weight: 1.0}
	}

	// Base vs thinking is incompatible regardless of other fields. This covers
	// both an explicit variant tag and a differing reasoning_effort setting.
	if baseVsThinking(a.Variant, b.Variant) || baseVsThinking(a.ReasoningEffort, b.ReasoningEffort) {
		return IdentityMatch{Level: MatchIncompatible, Contributes: false, Weight: 0}
	}

	// Version/release-date differences with same family => family only.
	if hasAny(diff, "version", "release_date") {
		return IdentityMatch{Level: MatchFamilyOnly, Contributes: false, Weight: 0, DifferingFields: diff}
	}

	// Other compatible variant differences (reasoning effort, preview/stable,
	// quantization, context mode, harness) => strong probable.
	return IdentityMatch{Level: MatchStrongProbable, Contributes: true, Weight: 0.5, MandatoryReview: true, DifferingFields: diff}
}

func hasAny(diff []string, fields ...string) bool {
	for _, d := range diff {
		for _, f := range fields {
			if d == f {
				return true
			}
		}
	}
	return false
}

func baseVsThinking(va, vb string) bool {
	norm := func(v string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		switch {
		case strings.Contains(v, "thinking") || strings.Contains(v, "reason") || v == "high" || v == "medium":
			return "thinking"
		case v == "base" || v == "instruct" || v == "low" || v == "minimal" || strings.Contains(v, "non-thinking"):
			return "base"
		}
		return ""
	}
	a, b := norm(va), norm(vb)
	return a != "" && b != "" && a != b
}

// CanonicalKey returns a stable key for the exact model identity (§19), used to
// recognize the same model across sources.
func (a ModelIdentity) CanonicalKey() string {
	creator, family, version := a.Creator, a.Family, a.Version
	if creator == "" {
		creator = a.Provider
	}
	if family == "" {
		family = a.Model
	}
	return strings.ToLower(strings.Join([]string{creator, family, version}, "|"))
}
