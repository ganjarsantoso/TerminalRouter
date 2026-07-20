package console

import (
	"testing"

	"github.com/termrouter/termrouter/internal/smart/external"
)

// TestParseSummaryCapturesModel verifies the summarizer extracts the
// independently-published model identity ("model") from the LLM output, which
// the external pipeline needs to perform variant matching (§18) instead of
// always comparing against the configured identity.
func TestParseSummaryCapturesModel(t *testing.T) {
	jsonText := `{
      "model": "gpt-5-preview",
      "capabilities": [
        {"capability": "reasoning", "score": 8.0, "confidence": 0.9, "evidence": "https://livebench.ai/x", "note": "n"}
      ],
      "confidence": 0.9,
      "sources": ["https://livebench.ai/x"]
    }`
	sum, err := parseSummary(jsonText)
	if err != nil {
		t.Fatalf("parseSummary: %v", err)
	}
	if sum.Model != "gpt-5-preview" {
		t.Fatalf("expected model gpt-5-preview, got %q", sum.Model)
	}
	if len(sum.Capabilities) != 1 || sum.Capabilities[0].Capability != external.CapReasoning {
		t.Fatalf("unexpected capabilities: %+v", sum.Capabilities)
	}

	// Empty/no model field should yield an empty Model without error.
	sum2, err := parseSummary(`{"capabilities":[{"capability":"coding","score":7.0}]}`)
	if err != nil {
		t.Fatalf("parseSummary (no model): %v", err)
	}
	if sum2.Model != "" {
		t.Fatalf("expected empty model, got %q", sum2.Model)
	}
}
