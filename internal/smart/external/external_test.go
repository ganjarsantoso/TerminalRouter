package external

import (
	"context"
	"testing"
	"time"
)

// mockSearcher returns canned search results so unit tests don't hit the network.
type mockSearcher struct{}

func (mockSearcher) Search(_ context.Context, _ string) ([]SearchResult, error) {
	return []SearchResult{
		{Title: "GPT-4o benchmarks", Snippet: "GPT-4o scores 72.1% on LiveBench overall and 68.4% on LiveBench reasoning. LiveBench math is 70.2%.", URL: "https://example.com/a"},
		{Title: "GPT-4o SWE-bench", Snippet: "GPT-4o achieves 51.0% on SWE-bench Verified.", URL: "https://example.com/b"},
		{Title: "GPT-4o Artificial Analysis", Snippet: "GPT-4o has an Artificial Analysis Intelligence Index of 19.", URL: "https://example.com/c"},
	}, nil
}

func newTestService() *Service {
	return NewService(nil, mockSearcher{})
}

func TestRegistryInfo(t *testing.T) {
	s := newTestService()
	info := s.RegistryInfo()
	if info.SourceCount != 4 {
		t.Fatalf("expected 4 sources, got %d", info.SourceCount)
	}
	if info.ModelCount == 0 {
		t.Fatalf("empty identity directory: models=%d", info.ModelCount)
	}
}

func TestResolveIdentity(t *testing.T) {
	cases := []struct {
		prov, model, wantID string
		want                bool
	}{
		{"openai", "gpt-4o", "openai-gpt-4o", true},
		{"anthropic", "claude-3-5-sonnet-latest", "anthropic-claude-3-5-sonnet", true},
		{"openai", "gpt-4o-mini", "openai-gpt-4o-mini", true},
		{"nope", "unknown-model", "", false},
	}
	for _, c := range cases {
		id, ok := ResolveIdentity(c.prov, c.model)
		if ok != c.want {
			t.Fatalf("ResolveIdentity(%s,%s) ok=%v want %v", c.prov, c.model, ok, c.want)
		}
		if ok && id.ID != c.wantID {
			t.Fatalf("resolved to %s want %s", id.ID, c.wantID)
		}
	}
}

func TestSearchGPT4o(t *testing.T) {
	s := newTestService()
	cp, ok := s.Search(context.Background(), "openai", "gpt-4o")
	if !ok {
		t.Fatal("gpt-4o not resolved")
	}
	if cp == nil {
		t.Fatal("no evidence")
	}
	for _, k := range []CapabilityKey{CapGeneral, CapReasoning, CapCoding, CapMathematics} {
		cc, has := cp.Capabilities[k]
		if !has {
			t.Fatalf("missing capability %s", k)
		}
		if cc.Estimate < 0 || cc.Estimate > 10 {
			t.Fatalf("%s estimate out of range: %v", k, cc.Estimate)
		}
		if cc.Confidence < 0 || cc.Confidence > 1 {
			t.Fatalf("%s confidence out of range: %v", k, cc.Confidence)
		}
	}
}

func TestBuildProposal(t *testing.T) {
	s := newTestService()
	p, ok := s.BuildProposal(context.Background(), "openai", "gpt-4o", map[string]float64{"general": 5.0})
	if !ok {
		t.Fatal("proposal not built")
	}
	if len(p.Fields) == 0 {
		t.Fatal("no proposal fields")
	}
	found := false
	for _, f := range p.Fields {
		if f.Capability == CapGeneral {
			if f.Current == nil || *f.Current != 5.0 {
				t.Fatalf("current general not captured: %v", f.Current)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("general field missing")
	}
	if p.Overall <= 0 || p.Confidence <= 0 || len(p.Sources) == 0 {
		t.Fatalf("proposal summary missing: overall=%.1f conf=%.2f sources=%v", p.Overall, p.Confidence, p.Sources)
	}
}

func TestExtractEvidence(t *testing.T) {
	id, _ := ResolveIdentity("openai", "gpt-4o")
	res := []SearchResult{
		{Title: "x", Snippet: "GPT-4o LiveBench overall 72.1% and LiveBench reasoning 68.4%", URL: "u"},
		{Title: "y", Snippet: "SWE-bench Verified 51.0% for gpt-4o", URL: "u2"},
	}
	recs := extractEvidence(id, res)
	if len(recs) < 2 {
		t.Fatalf("expected >=2 evidence records, got %d", len(recs))
	}
}

var _ = time.Now
