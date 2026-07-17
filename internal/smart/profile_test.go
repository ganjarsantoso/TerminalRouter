package smart

import "testing"

func TestBuiltinLookup(t *testing.T) {
	p, ok := LookupBuiltin("deepseek/deepseek-chat")
	if !ok {
		t.Fatal("missing builtin")
	}
	if p.Cap(CapCoding) < 4 {
		t.Fatalf("coding=%d", p.Cap(CapCoding))
	}
}

func TestUserOverridePrecedence(t *testing.T) {
	user := map[string]ModelProfile{
		"deepseek/deepseek-chat": {
			ID: "deepseek/deepseek-chat", Source: SourceUser,
			Capabilities: map[string]int{CapCoding: 1},
			Properties:   ModelProperties{CostTier: 5},
		},
	}
	ps := NewProfileStore(user, true)
	p, found := ps.Resolve("deepseek", "deepseek-chat", "")
	if !found {
		t.Fatal("expected found")
	}
	if p.Source != SourceUser {
		t.Fatalf("source=%s", p.Source)
	}
	if p.Cap(CapCoding) != 1 {
		t.Fatalf("override not applied: %d", p.Cap(CapCoding))
	}
}

func TestValidateProfile(t *testing.T) {
	err := ValidateProfile(ModelProfile{Capabilities: map[string]int{CapCoding: 9}})
	if err == nil {
		t.Fatal("expected error")
	}
}
