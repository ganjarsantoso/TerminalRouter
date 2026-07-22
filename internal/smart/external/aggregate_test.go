package external

import (
	"fmt"
	"testing"
)

func recFor(cap CapabilityKey, src SourceID, bench string, val float64, published ModelIdentity) EvidenceRecord {
	return EvidenceRecord{
		Source:     src,
		Benchmark:  bench,
		Value:      val,
		Scale:      ScaleZeroToHundred,
		Capability: cap,
		Published:  published,
	}
}

func TestBuildConsensusExcludesIncompatible(t *testing.T) {
	id := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", ReasoningEffort: "base", EndpointProvider: "openai"}
	thinking := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", ReasoningEffort: "high", EndpointProvider: "openai"}
	recs := []EvidenceRecord{
		recFor(CapReasoning, SourceLiveBench, "livebench/reasoning", 70, id),
		recFor(CapReasoning, SourceLMArena, "lmarena/quality", 80, thinking), // incompatible
	}
	cp := buildConsensus(id, recs)
	cc := cp.Capabilities[CapReasoning]
	if cc.SourceCount != 1 {
		t.Fatalf("expected 1 contributing record, got %d", cc.SourceCount)
	}
	if cc.ExcludedCount != 1 {
		t.Fatalf("expected 1 excluded, got %d", cc.ExcludedCount)
	}
}

func TestBuildConsensusStrongProbableReview(t *testing.T) {
	id := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", PreviewStable: "stable", EndpointProvider: "openai"}
	preview := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", PreviewStable: "preview", EndpointProvider: "openai"}
	recs := []EvidenceRecord{
		recFor(CapReasoning, SourceLiveBench, "livebench/reasoning", 70, preview),
	}
	cp := buildConsensus(id, recs)
	cc := cp.Capabilities[CapReasoning]
	if !cc.MandatoryReview {
		t.Fatalf("strong_probable should set mandatory review")
	}
	if cc.SourceCount != 1 {
		t.Fatalf("expected contributing, got %d", cc.SourceCount)
	}
	if len(cc.Contributing) == 1 && cc.Contributing[0].Weight != 0.5 {
		t.Fatalf("expected weight 0.5, got %f", cc.Contributing[0].Weight)
	}
}

func TestBuildConsensusExperimentDedup(t *testing.T) {
	id := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", EndpointProvider: "openai"}
	// r1 and r2 are the SAME experiment published on two sites (same benchmark,
	// harness, score, original publisher) -> must collapse to one, with the
	// mirror folded into provenance.
	r1 := recFor(CapMathematics, SourceLiveBench, "livebench/math", 75, id)
	r1.URL = "https://livebench.ai/gpt5"
	r1.Harness = "livebench"
	r1.OriginalPublisher = "livebench"
	r2 := recFor(CapMathematics, SourceLMArena, "livebench/math", 75, id)
	r2.URL = "https://lmarena.ai/mirror/gpt5"
	r2.Harness = "livebench"
	r2.OriginalPublisher = "livebench"
	// Third distinct record (different benchmark subtask).
	r3 := recFor(CapMathematics, SourceLiveBench, "livebench/math2", 60, id)
	r3.Harness = "livebench"

	recs := []EvidenceRecord{r1, r2, r3}
	cp := buildConsensus(id, recs)
	cc := cp.Capabilities[CapMathematics]
	if cc.SourceCount != 2 {
		t.Fatalf("expected 2 contributing records after dedup, got %d (%+v)", cc.SourceCount, cc.Contributing)
	}
	// Find the deduped record and confirm provenance captured the mirror URL.
	found := false
	for _, c := range cc.Contributing {
		for _, u := range c.Evidence.ProvenanceURLs {
			if u == r2.URL {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("mirror URL should be folded into provenance")
	}
}

func TestContributionCapsLimitPerSource(t *testing.T) {
	id := ModelIdentity{Creator: "OpenAI", Family: "GPT-5", Version: "5", EndpointProvider: "openai"}
	var recs []EvidenceRecord
	// 10 distinct subtasks from a single source on the same benchmark family.
	// Each is a distinct experiment (unique harness) but shares source + family.
	for i := 0; i < 10; i++ {
		r := recFor(CapReasoning, SourceLiveBench, "livebench/reasoning", 50+float64(i), id)
		r.Harness = fmt.Sprintf("livebench-h%d", i)
		recs = append(recs, r)
	}
	cp := buildConsensus(id, recs)
	cc := cp.Capabilities[CapReasoning]
	// maxPerSource = 4 -> capped at 4, 6 excluded by caps.
	if cc.SourceCount > 4 {
		t.Fatalf("expected source capped at 4, got %d", cc.SourceCount)
	}
	if cc.ExcludedCount < 6 {
		t.Fatalf("expected at least 6 excluded by caps, got %d", cc.ExcludedCount)
	}
}
