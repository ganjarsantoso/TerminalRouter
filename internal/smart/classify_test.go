package smart

import (
	"testing"

	"github.com/termrouter/termrouter/internal/normalization"
)

func TestClassifyCodingDebug(t *testing.T) {
	task := ClassifyPrompt("Find the concurrency bug in this Go worker pool.\n```go\nfunc worker() { ... }\n```\npanic: concurrent map writes")
	if task.PrimaryType != TypeCodingDebug {
		t.Fatalf("primary=%s want coding_debug", task.PrimaryType)
	}
	if task.Requirements[CapCoding] < 4 {
		t.Fatalf("coding req=%d", task.Requirements[CapCoding])
	}
	if task.Confidence < 0.5 {
		t.Fatalf("confidence too low: %f", task.Confidence)
	}
}

func TestClassifySimpleGreeting(t *testing.T) {
	task := ClassifyPrompt("hello")
	if task.PrimaryType != TypeGeneralChat {
		t.Fatalf("primary=%s", task.PrimaryType)
	}
	if task.Complexity != ComplexitySimple {
		t.Fatalf("complexity=%s", task.Complexity)
	}
}

func TestClassifyExplainDoesNotForceCoding(t *testing.T) {
	task := ClassifyPrompt("Explain Kubernetes to a five-year-old")
	if task.PrimaryType == TypeCodingDebug || task.PrimaryType == TypeCodingGeneration {
		t.Fatalf("should not be coding, got %s", task.PrimaryType)
	}
	if task.Complexity == ComplexityComplex {
		t.Fatalf("should not be complex: %s", task.Complexity)
	}
}

func TestClassifyToolsRequired(t *testing.T) {
	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{{
			Role: normalization.RoleUser,
			Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "Call the weather tool for Paris"}},
		}},
		Tools: []normalization.Tool{{Name: "weather"}},
		ToolChoice: &normalization.ToolChoice{Type: "required"},
	}
	task := Classify(req)
	if !task.HardRequirements.Tools {
		t.Fatal("tools should be hard required")
	}
}

func TestClassifyDeterministic(t *testing.T) {
	p := "Review this concurrent Go function for race conditions"
	a := ClassifyPrompt(p)
	b := ClassifyPrompt(p)
	if a.PrimaryType != b.PrimaryType || a.Complexity != b.Complexity {
		t.Fatalf("non-deterministic: %+v vs %+v", a, b)
	}
	if a.Confidence != b.Confidence {
		t.Fatalf("confidence mismatch %f vs %f", a.Confidence, b.Confidence)
	}
}
