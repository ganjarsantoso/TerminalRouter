package external

import (
	"context"
	"testing"
)

type stubSummarizer struct {
	summary Summary
}

func (s *stubSummarizer) SummarizeEvidence(ctx context.Context, modelName string, pages []PageText) (Summary, error) {
	return s.summary, nil
}

type stubSearcher struct {
	results []SearchResult
}

func (s *stubSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	return s.results, nil
}

// TestParsePublishedIdentity verifies the independent published-identity
// extraction used by the LLM summarization path (§18). The published identity
// must be derived from what the source reports, not copied from the configured
// identity.
func TestParsePublishedIdentity(t *testing.T) {
	// Empty name -> zero identity (Match treats as exact, legacy behaviour).
	if got := parsePublishedIdentity(""); got.ID != "" {
		t.Fatalf("empty name should yield zero identity, got %+v", got)
	}

	// Provider/model prefix with a preview variant: the variant suffix must be
	// stripped from Family so it compares against the base model.
	p := parsePublishedIdentity("openai/gpt-5-preview")
	if p.Creator != "openai" {
		t.Fatalf("expected creator openai, got %q", p.Creator)
	}
	if p.Family != "gpt-5" {
		t.Fatalf("expected family gpt-5 (variant stripped), got %q", p.Family)
	}
	if p.PreviewStable != "preview" {
		t.Fatalf("expected preview flag, got %q", p.PreviewStable)
	}

	// Base vs thinking reasoning-effort hint.
	if r := parsePublishedIdentity("deepseek-reasoner"); r.ReasoningEffort != "high" {
		t.Fatalf("expected reasoning effort high, got %q", r.ReasoningEffort)
	}
	if r := parsePublishedIdentity("claude-base"); r.ReasoningEffort != "base" {
		t.Fatalf("expected reasoning effort base, got %q", r.ReasoningEffort)
	}
}

// TestSummarizeEvidenceIndependentPublishedIdentity verifies that the summarizer
// output's reported model name becomes the published identity, so a preview
// variant found in evidence is NOT silently credited to the base configured
// model (which would have made variant matching report "exact").
func TestSummarizeEvidenceIndependentPublishedIdentity(t *testing.T) {
	id := identityFor("openai", "gpt-5")
	sum := &stubSummarizer{summary: Summary{
		Model: "gpt-5-preview",
		Capabilities: []SummaryCapability{
			{Capability: CapReasoning, Score: 8.0, Evidence: "https://livebench.ai/gpt5-preview"},
		},
	}}
	srch := &stubSearcher{results: []SearchResult{
		{Snippet: "GPT-5 preview scores", URL: "https://livebench.ai/gpt5-preview"},
	}}

	recs := summarizeEvidence(context.Background(), sum, srch, id, []SearchResult{
		{Snippet: "GPT-5 preview scores", URL: "https://livebench.ai/gpt5-preview"},
	}, false)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	pub := recs[0].Published
	if pub.PreviewStable != "preview" {
		t.Fatalf("published identity should carry preview flag, got %+v", pub)
	}
	// The published preview must differ from the configured base model.
	if recs[0].Published.ID == id.ID && pub.PreviewStable == id.PreviewStable {
		t.Fatalf("published identity must not equal configured identity for a preview variant")
	}

	// Consensus must flag mandatory review (strong-probable variant match).
	cp := buildConsensus(id, recs)
	cc, ok := cp.Capabilities[CapReasoning]
	if !ok {
		t.Fatal("expected reasoning capability in consensus")
	}
	if !cc.MandatoryReview {
		t.Fatal("expected mandatory review when evidence is about a preview variant of the configured base model")
	}
}

// TestSummarizeEvidenceSameModelStaysExact verifies that when the evidence
// confirms the exact configured model, the published identity matches and no
// mandatory review is triggered.
func TestSummarizeEvidenceSameModelStaysExact(t *testing.T) {
	id := identityFor("openai", "gpt-5")
	sum := &stubSummarizer{summary: Summary{
		Model: "gpt-5",
		Capabilities: []SummaryCapability{
			{Capability: CapReasoning, Score: 8.0, Evidence: "https://livebench.ai/gpt5"},
		},
	}}
	srch := &stubSearcher{results: []SearchResult{
		{Snippet: "GPT-5 scores", URL: "https://livebench.ai/gpt5"},
	}}
	recs := summarizeEvidence(context.Background(), sum, srch, id, []SearchResult{
		{Snippet: "GPT-5 scores", URL: "https://livebench.ai/gpt5"},
	}, false)
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	cp := buildConsensus(id, recs)
	cc := cp.Capabilities[CapReasoning]
	if cc.MandatoryReview {
		t.Fatal("exact same-model evidence should not require mandatory review")
	}
}
