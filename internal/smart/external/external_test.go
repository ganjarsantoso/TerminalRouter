package external

import "testing"

func TestRegistryInfo(t *testing.T) {
	s := NewService(nil)
	info := s.RegistryInfo()
	if info.SourceCount != 4 {
		t.Fatalf("expected 4 sources, got %d", info.SourceCount)
	}
	if info.ModelCount == 0 || info.EvidenceCount == 0 {
		t.Fatalf("empty registry: models=%d evidence=%d", info.ModelCount, info.EvidenceCount)
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
	s := NewService(nil)
	cp, ok := s.Search("openai", "gpt-4o")
	if !ok {
		t.Fatal("gpt-4o not resolved")
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
	s := NewService(nil)
	p, ok := s.BuildProposal("openai", "gpt-4o", map[string]float64{"general": 5.0})
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
}
