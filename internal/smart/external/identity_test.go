package external

import (
	"testing"
)

func TestMatchExact(t *testing.T) {
	base := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", ReleaseDate: "2025-08-01", EndpointProvider: "openai"}
	// Published identical -> exact.
	got := base.Match(base)
	if got.Level != MatchExact {
		t.Fatalf("expected exact, got %s", got.Level)
	}
	if !got.Contributes {
		t.Fatalf("exact should contribute")
	}
	if got.Weight != 1.0 {
		t.Fatalf("exact weight should be 1.0, got %f", got.Weight)
	}
	// Zero published identity -> exact (preserve existing behaviour).
	if got := base.Match(ModelIdentity{}); got.Level != MatchExact {
		t.Fatalf("zero published identity should be exact, got %s", got.Level)
	}
}

func TestMatchBaseVsThinkingIncompatible(t *testing.T) {
	base := ModelIdentity{Creator: "Anthropic", Family: "Claude-Opus-4", Version: "4", ReasoningEffort: "base", EndpointProvider: "anthropic"}
	thinking := ModelIdentity{Creator: "Anthropic", Family: "Claude-Opus-4", Version: "4", ReasoningEffort: "high", EndpointProvider: "anthropic"}
	got := base.Match(thinking)
	if got.Level != MatchIncompatible {
		t.Fatalf("base vs thinking must be incompatible, got %s", got.Level)
	}
	if got.Contributes {
		t.Fatalf("incompatible must not contribute")
	}
}

func TestMatchVersionDiffFamilyOnly(t *testing.T) {
	base := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", EndpointProvider: "openai"}
	other := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5.1", EndpointProvider: "openai"}
	got := base.Match(other)
	if got.Level != MatchFamilyOnly {
		t.Fatalf("version diff must be family_only, got %s", got.Level)
	}
	if got.Contributes {
		t.Fatalf("family_only must not contribute by default")
	}
}

func TestMatchReasoningEffortStrongProbable(t *testing.T) {
	base := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", PreviewStable: "stable", EndpointProvider: "openai"}
	other := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", PreviewStable: "preview", EndpointProvider: "openai"}
	got := base.Match(other)
	if got.Level != MatchStrongProbable {
		t.Fatalf("preview vs stable must be strong_probable, got %s", got.Level)
	}
	if !got.Contributes {
		t.Fatalf("strong_probable should contribute")
	}
	if got.Weight != 0.5 {
		t.Fatalf("strong_probable weight should be 0.5, got %f", got.Weight)
	}
	if !got.MandatoryReview {
		t.Fatalf("strong_probable must require mandatory review")
	}
}

func TestCanonicalKeyStable(t *testing.T) {
	a := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", PreviewStable: "stable", EndpointProvider: "openai"}
	b := ModelIdentity{Creator: "openai", Family: "gpt-5", Version: "5", PreviewStable: "STABLE", EndpointProvider: "OpenAI"}
	if a.CanonicalKey() != b.CanonicalKey() {
		t.Fatalf("case/ordering should not change canonical key: %q vs %q", a.CanonicalKey(), b.CanonicalKey())
	}
}
