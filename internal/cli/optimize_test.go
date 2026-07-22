package cli

import (
	"testing"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
)

func TestSplitComma(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"single", "off", 1},
		{"two", "off,safe", 2},
		{"three", "off,safe,balanced", 3},
		{"four", "off,safe,balanced,aggressive", 4},
		{"trailing comma", "off,safe,", 2},
		{"leading comma", ",off,safe", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitComma(tc.in)
			if len(got) != tc.want {
				t.Fatalf("splitComma(%q): got %d items %v, want %d", tc.in, len(got), got, tc.want)
			}
		})
	}
}

func TestSplitCommaValues(t *testing.T) {
	got := splitComma("a,b,c")
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected values: %v", got)
	}
}

func TestParseDurationLast(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		wantOk bool
	}{
		{"24h", "24h", true},
		{"7d", "168h", true},
		{"30m", "30m", true},
		{"invalid", "abc", false},
		{"zero", "0s", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDurationLast(tc.input)
			if tc.wantOk && err != nil {
				t.Fatalf("parseDurationLast(%q): unexpected error: %v", tc.input, err)
			}
			if !tc.wantOk && err == nil {
				t.Fatalf("parseDurationLast(%q): expected error", tc.input)
			}
		})
	}
}

func TestCloneNormalized(t *testing.T) {
	orig := &normalization.NormalizedRequest{
		ID:             "test-id",
		RequestedModel: "openai/gpt-4o",
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: "hello"},
			}},
		},
	}
	clone := cloneNormalized(orig)
	if clone.ID != orig.ID {
		t.Fatalf("ID mismatch: %s vs %s", clone.ID, orig.ID)
	}
	if clone.Messages[0].Content[0].Text != "hello" {
		t.Fatalf("message text mismatch")
	}
	clone.ID = "modified"
	if orig.ID == "modified" {
		t.Fatal("modifying clone affected original")
	}
}

func TestOptimizationContextForCLI(t *testing.T) {
	req := &normalization.NormalizedRequest{ID: "req-1"}

	oc := optimizationContextForCLI(req, "openai/gpt-4o", "safe")
	if oc.ProviderID != "openai" || oc.ModelID != "gpt-4o" {
		t.Fatalf("expected openai/gpt-4o, got %s/%s", oc.ProviderID, oc.ModelID)
	}
	if oc.ClientPreference != "safe" {
		t.Fatalf("expected safe, got %s", oc.ClientPreference)
	}

	oc2 := optimizationContextForCLI(req, "gpt-4o", "balanced")
	if oc2.ProviderID != "" || oc2.ModelID != "gpt-4o" {
		t.Fatalf("expected empty provider, got %s/%s", oc2.ProviderID, oc2.ModelID)
	}

	oc3 := optimizationContextForCLI(req, "", "off")
	if oc3.ProviderID != "" || oc3.ModelID != "" {
		t.Fatalf("expected empty, got %s/%s", oc3.ProviderID, oc3.ModelID)
	}
}

func TestSplitModel(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantProv string
		wantOk   bool
	}{
		{"provider/model", "openai/gpt-4o", "openai", true},
		{"model only", "gpt-4o", "", false},
		{"multi-slash", "a/b/c", "a", true},
		{"empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov, ok := splitModel(tc.input)
			if prov != tc.wantProv || ok != tc.wantOk {
				t.Fatalf("splitModel(%q): got (%q, %v), want (%q, %v)", tc.input, prov, ok, tc.wantProv, tc.wantOk)
			}
		})
	}
}

func TestFormatToRenderer(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"human", "human"},
		{"compact-json", "compact_json"},
		{"tagged-text", "tagged_text"},
		{"tagged_text", "tagged_text"},
		{"native-prompt", "native_prompt"},
		{"native_prompt", "native_prompt"},
		{"compact_json", "compact_json"},
		{"unknown", "compact_json"},
		{"", "compact_json"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := formatToRenderer(tc.input)
			if got != tc.want {
				t.Fatalf("formatToRenderer(%q): got %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestOptimizationContextForCLIWithConfig(t *testing.T) {
	_ = config.DefaultOptimization()
	req := &normalization.NormalizedRequest{ID: "test"}
	oc := optimizationContextForCLI(req, "anthropic/claude-3-opus", "aggressive")
	if oc.ProviderID != "anthropic" {
		t.Fatalf("expected anthropic, got %s", oc.ProviderID)
	}
	if oc.ModelID != "claude-3-opus" {
		t.Fatalf("expected claude-3-opus, got %s", oc.ModelID)
	}
}
