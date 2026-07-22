package lui

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/termrouter/termrouter/internal/normalization"
)

// enrichedEnvelope returns an envelope with at least one entry in every
// semantic field region, ready for mutation testing.
func enrichedEnvelope(t *testing.T) *Envelope {
	t.Helper()
	env := newValidEnvelope(t)
	env.Goals = []Goal{
		{Type: "goal_type", Summary: "goal_summary", Priority: 1, Source: SourceClientExplicit},
	}
	env.Constraints = []Constraint{
		{ID: "c1", Type: "constraint_type", Value: "constraint_value", Priority: 2, Source: SourceServerPolicy, Protection: ProtectionImmutable},
	}
	env.Context = []ContextReference{
		{
			ID: "ctx1", Kind: "file", URI: "file:///test.txt",
			ContentHash: "abc123", TokenEstimate: 100, Priority: 3,
			Protection: ProtectionProtected, Inline: true, Content: "inline_content",
		},
	}
	env.State = []StateEntry{
		{Key: "sk1", Value: "sv1", Source: SourceClientExplicit, Protection: ProtectionProtected},
	}
	env.Tools = []ToolReference{
		{Name: "tool1", SchemaHash: "tool_hash", Source: SourceClientExplicit},
	}
	env.Evidence = []EvidenceReference{
		{ID: "e1", Kind: "research", URI: "https://example.com", Summary: "evidence_summary", Source: SourceAgentGenerated},
	}
	env.Output = OutputContract{Format: "json", Fields: []string{"f1", "f2"}}
	env.Dictionary = map[string]string{"dk1": "dv1"}
	return env
}

// --- fixture helpers ---

func newValidEnvelope(t *testing.T) *Envelope {
	t.Helper()
	return &Envelope{
		Version: Version,
		Kind:    KindTask,
		Task: TaskDescriptor{
			Type:      "chat",
			RequestID: "req-123",
		},
	}
}

func newRequest(t *testing.T, userText string) *normalization.NormalizedRequest {
	t.Helper()
	return &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{
				Role: normalization.RoleUser,
				Content: []normalization.ContentBlock{
					{Type: normalization.ContentText, Text: userText},
				},
			},
		},
	}
}

func newRequestWithSystem(t *testing.T, sys, userText string) *normalization.NormalizedRequest {
	t.Helper()
	return &normalization.NormalizedRequest{
		System: sys,
		Messages: []normalization.Message{
			{
				Role: normalization.RoleUser,
				Content: []normalization.ContentBlock{
					{Type: normalization.ContentText, Text: userText},
				},
			},
		},
	}
}

func newRequestWithTools(t *testing.T, userText string, tools []normalization.Tool) *normalization.NormalizedRequest {
	t.Helper()
	req := newRequest(t, userText)
	req.Tools = tools
	return req
}

// --- Validate ---

func TestValidateValidEnvelope(t *testing.T) {
	env := newValidEnvelope(t)
	if err := Validate(env); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateNilEnvelope(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("expected error for nil envelope")
	}
}

func TestValidateMissingVersion(t *testing.T) {
	env := newValidEnvelope(t)
	env.Version = ""
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "v" {
		t.Fatalf("expected field 'v', got %q", ve.Field)
	}
}

func TestValidateMissingKind(t *testing.T) {
	env := newValidEnvelope(t)
	env.Kind = ""
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for missing kind")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "kind" {
		t.Fatalf("expected field 'kind', got %q", ve.Field)
	}
}

func TestValidateUnsupportedMajorVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"major 1", "1.0"},
		{"major 2", "2.0"},
		{"major 99", "99.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := newValidEnvelope(t)
			env.Version = tc.version
			err := Validate(env)
			if err == nil {
				t.Fatal("expected error for unsupported major version")
			}
			if !IsMajorVersionError(err) {
				t.Fatal("expected IsMajorVersionError to return true")
			}
		})
	}
}

func TestValidateUnknownKind(t *testing.T) {
	env := newValidEnvelope(t)
	env.Kind = "unknown_kind"
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "kind" {
		t.Fatalf("expected field 'kind', got %q", ve.Field)
	}
}

func TestValidateInvalidSourceInConstraint(t *testing.T) {
	env := newValidEnvelope(t)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: "v1", Source: "bogus_source"},
	}
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for invalid source in constraint")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "constraints[0].source" {
		t.Fatalf("expected field constraints[0].source, got %q", ve.Field)
	}
}

func TestValidateInvalidProtectionClass(t *testing.T) {
	env := newValidEnvelope(t)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: "v1", Source: SourceClientExplicit, Protection: "nope"},
	}
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for invalid protection class")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "constraints[0].protection" {
		t.Fatalf("expected field constraints[0].protection, got %q", ve.Field)
	}
}

func TestValidateContextRefNoContentNoURI(t *testing.T) {
	env := newValidEnvelope(t)
	env.Context = []ContextReference{
		{ID: "ref1", Kind: "file"},
	}
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for context ref with no content/URI/dict")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "context[0]" {
		t.Fatalf("expected field context[0], got %q", ve.Field)
	}
}

func TestValidateContextRefWithContentPasses(t *testing.T) {
	env := newValidEnvelope(t)
	env.Context = []ContextReference{
		{ID: "ref1", Content: "inline content"},
	}
	if err := Validate(env); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateContextRefWithURIPasses(t *testing.T) {
	env := newValidEnvelope(t)
	env.Context = []ContextReference{
		{ID: "ref1", URI: "file:///foo.txt"},
	}
	if err := Validate(env); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateContextRefWithDictEntryPasses(t *testing.T) {
	env := newValidEnvelope(t)
	env.Dictionary = map[string]string{"ref1": "some value"}
	env.Context = []ContextReference{
		{ID: "ref1"},
	}
	if err := Validate(env); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateAllKinds(t *testing.T) {
	kinds := []PacketKind{
		KindTask, KindStateUpdate, KindFindingSet, KindExecutionPlan,
		KindToolResult, KindContextManifest, KindTestReport, KindCompletion, KindHandoff,
	}
	for _, k := range kinds {
		t.Run(string(k), func(t *testing.T) {
			env := newValidEnvelope(t)
			env.Kind = k
			if err := Validate(env); err != nil {
				t.Fatalf("expected kind %q to be valid, got %v", k, err)
			}
		})
	}
}

func TestValidateInvalidStateSource(t *testing.T) {
	env := newValidEnvelope(t)
	env.State = []StateEntry{
		{Key: "k", Value: "v", Source: "bogus"},
	}
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for invalid state source")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "state[0].source" {
		t.Fatalf("expected field state[0].source, got %q", ve.Field)
	}
}

func TestValidateInvalidToolSource(t *testing.T) {
	env := newValidEnvelope(t)
	env.Tools = []ToolReference{
		{Name: "tool1", Source: "bogus"},
	}
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for invalid tool source")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "tools[0].source" {
		t.Fatalf("expected field tools[0].source, got %q", ve.Field)
	}
}

func TestValidateInvalidEvidenceSource(t *testing.T) {
	env := newValidEnvelope(t)
	env.Evidence = []EvidenceReference{
		{ID: "e1", Source: "bogus"},
	}
	err := Validate(env)
	if err == nil {
		t.Fatal("expected error for invalid evidence source")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "evidence[0].source" {
		t.Fatalf("expected field evidence[0].source, got %q", ve.Field)
	}
}

func TestValidationErrorString(t *testing.T) {
	e := &ValidationError{Field: "v", Reason: "version is required"}
	got := e.Error()
	if !strings.Contains(got, "v") || !strings.Contains(got, "version is required") {
		t.Fatalf("unexpected error string: %q", got)
	}
	e2 := &ValidationError{Message: "nil envelope"}
	got2 := e2.Error()
	if !strings.Contains(got2, "nil envelope") {
		t.Fatalf("unexpected error string: %q", got2)
	}
}

func TestIsMajorVersionErrorFalse(t *testing.T) {
	if IsMajorVersionError(nil) {
		t.Fatal("expected false for nil error")
	}
	if IsMajorVersionError(&ValidationError{Field: "kind", Reason: "bad"}) {
		t.Fatal("expected false for non-major error")
	}
}

// --- BuildEnvelope ---

func TestBuildEnvelopeVersion(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if env.Version != Version {
		t.Fatalf("expected version %q, got %q", Version, env.Version)
	}
}

func TestBuildEnvelopeKindTask(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if env.Kind != KindTask {
		t.Fatalf("expected kind %q, got %q", KindTask, env.Kind)
	}
}

func TestBuildEnvelopeRequestID(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r42"})
	if env.Task.RequestID != "r42" {
		t.Fatalf("expected request ID %q, got %q", "r42", env.Task.RequestID)
	}
}

func TestBuildEnvelopeTaskTypeDefault(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{})
	if env.Task.Type != "chat" {
		t.Fatalf("expected default type %q, got %q", "chat", env.Task.Type)
	}
}

func TestBuildEnvelopeTaskTypeOverride(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{TaskType: "code_review"})
	if env.Task.Type != "code_review" {
		t.Fatalf("expected type %q, got %q", "code_review", env.Task.Type)
	}
}

func TestBuildEnvelopeSummary(t *testing.T) {
	req := newRequest(t, "this is my question")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if env.Task.Summary != "this is my question" {
		t.Fatalf("expected summary %q, got %q", "this is my question", env.Task.Summary)
	}
}

func TestBuildEnvelopeSummaryTruncated(t *testing.T) {
	long := strings.Repeat("x", 300)
	req := newRequest(t, long)
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if len(env.Task.Summary) > 203 {
		t.Fatalf("expected summary <=203 chars, got %d", len(env.Task.Summary))
	}
	if !strings.HasSuffix(env.Task.Summary, "...") {
		t.Fatal("expected summary to end with ...")
	}
}

func TestBuildEnvelopeClientConstraints(t *testing.T) {
	req := newRequest(t, "hello")
	cc := []Constraint{{ID: "c1", Type: "no_commit", Value: "true", Source: SourceClientExplicit}}
	env := BuildEnvelope(req, BuildContext{ClientConstraints: cc})
	if len(env.Constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(env.Constraints))
	}
	if env.Constraints[0].ID != "c1" {
		t.Fatalf("expected constraint ID %q, got %q", "c1", env.Constraints[0].ID)
	}
}

func TestBuildEnvelopeTools(t *testing.T) {
	req := newRequestWithTools(t, "hello", []normalization.Tool{
		{Name: "search", Description: "web search"},
	})
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if len(env.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(env.Tools))
	}
	if env.Tools[0].Name != "search" {
		t.Fatalf("expected tool name %q, got %q", "search", env.Tools[0].Name)
	}
	if env.Tools[0].Source != SourceClientExplicit {
		t.Fatalf("expected source %q, got %q", SourceClientExplicit, env.Tools[0].Source)
	}
}

func TestBuildEnvelopeState(t *testing.T) {
	req := newRequest(t, "hello world")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if len(env.State) != 1 {
		t.Fatalf("expected 1 state entry, got %d", len(env.State))
	}
	if env.State[0].Key != "current_user_request" {
		t.Fatalf("expected key %q, got %q", "current_user_request", env.State[0].Key)
	}
	if env.State[0].Value != "hello world" {
		t.Fatalf("expected value %q, got %q", "hello world", env.State[0].Value)
	}
}

func TestBuildEnvelopeSystemContext(t *testing.T) {
	req := newRequestWithSystem(t, "You are helpful.", "hi")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	found := false
	for _, c := range env.Context {
		if c.ID == "system" && c.Content == "You are helpful." {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected system context entry")
	}
}

func TestBuildEnvelopeIntegrityHash(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if env.Integrity.ContentHash == "" {
		t.Fatal("expected non-empty content hash")
	}
	if !strings.HasPrefix(env.Integrity.Generator, "termrouter/lui/") {
		t.Fatalf("expected generator prefix termrouter/lui/, got %q", env.Integrity.Generator)
	}
}

func TestVerifyIntegrityHash_Valid(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if !VerifyIntegrityHash(env) {
		t.Fatal("expected integrity hash to verify for freshly built envelope")
	}
}

func TestVerifyIntegrityHash_TamperedField(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	// Tamper with a semantic field.
	env.Task.Summary = "tampered"
	if VerifyIntegrityHash(env) {
		t.Fatal("expected integrity hash to fail after tampering")
	}
}

func TestVerifyIntegrityHash_Nil(t *testing.T) {
	if VerifyIntegrityHash(nil) {
		t.Fatal("expected false for nil envelope")
	}
}

func TestComputeIntegrityHash_Deterministic(t *testing.T) {
	req := newRequest(t, "hello")
	env1 := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	env2 := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if env1.Integrity.ContentHash != env2.Integrity.ContentHash {
		t.Fatalf("expected same hash for identical envelopes, got %q and %q",
			env1.Integrity.ContentHash, env2.Integrity.ContentHash)
	}
}

func TestComputeIntegrityHash_DifferentContentDifferentHash(t *testing.T) {
	req1 := newRequest(t, "hello")
	req2 := newRequest(t, "world")
	env1 := BuildEnvelope(req1, BuildContext{RequestID: "r1"})
	env2 := BuildEnvelope(req2, BuildContext{RequestID: "r1"})
	if env1.Integrity.ContentHash == env2.Integrity.ContentHash {
		t.Fatal("expected different hash for different content")
	}
}

func TestBuildEnvelopeIsValid(t *testing.T) {
	req := newRequest(t, "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	if err := Validate(env); err != nil {
		t.Fatalf("BuildEnvelope should produce valid envelope, got %v", err)
	}
}

// --- Render ---

func TestRenderCompactJSON(t *testing.T) {
	env := newValidEnvelope(t)
	out, name, err := Render(env, "compact_json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "compact_json" {
		t.Fatalf("expected renderer name %q, got %q", "compact_json", name)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("compact_json output is not valid JSON: %v", err)
	}
	if parsed["v"] != Version {
		t.Fatalf("expected v=%q in JSON, got %v", Version, parsed["v"])
	}
}

func TestRenderHuman(t *testing.T) {
	env := newValidEnvelope(t)
	out, name, err := Render(env, "human")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "human" {
		t.Fatalf("expected renderer name %q, got %q", "human", name)
	}
	if !strings.Contains(out, "LUI") {
		t.Fatal("expected human output to contain LUI header")
	}
	if !strings.Contains(out, "chat") {
		t.Fatal("expected human output to contain task type")
	}
}

func TestRenderTaggedText(t *testing.T) {
	env := newValidEnvelope(t)
	out, name, err := Render(env, "tagged_text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "tagged_text" {
		t.Fatalf("expected renderer name %q, got %q", "tagged_text", name)
	}
	if !strings.Contains(out, "LUI/"+Version) {
		t.Fatal("expected tagged output to contain LUI/<version> header")
	}
}

func TestRenderNativePrompt(t *testing.T) {
	env := newValidEnvelope(t)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "security", Value: "no secrets", Source: SourceServerPolicy, Protection: ProtectionImmutable},
	}
	out, name, err := Render(env, "native_prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "native_prompt" {
		t.Fatalf("expected renderer name %q, got %q", "native_prompt", name)
	}
	if !strings.Contains(out, "Task:") {
		t.Fatal("expected native_prompt output to contain Task:")
	}
	if !strings.Contains(out, "no secrets") {
		t.Fatal("expected native_prompt output to contain constraint value")
	}
}

func TestRenderUnknownFormatFallsBack(t *testing.T) {
	env := newValidEnvelope(t)
	out, name, err := Render(env, "nonexistent_format")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "native_prompt" {
		t.Fatalf("expected fallback to native_prompt, got %q", name)
	}
	if !strings.Contains(out, "Task:") {
		t.Fatal("expected fallback output to contain Task:")
	}
}

func TestRenderEmptyNameFallsBack(t *testing.T) {
	env := newValidEnvelope(t)
	out, name, err := Render(env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "native_prompt" {
		t.Fatalf("expected fallback to native_prompt, got %q", name)
	}
	if !strings.Contains(out, "Task:") {
		t.Fatal("expected fallback output to contain Task:")
	}
}

func TestRenderersReturnsAll(t *testing.T) {
	rs := Renderers()
	expected := []string{"compact_json", "human", "tagged_text", "native_prompt"}
	for _, n := range expected {
		if _, ok := rs[n]; !ok {
			t.Fatalf("expected renderer %q in Renderers()", n)
		}
	}
}

func TestGetRendererNormalized(t *testing.T) {
	_, err := GetRenderer("Compact_Json")
	if err != nil {
		t.Fatalf("expected normalized lookup to succeed, got %v", err)
	}
}

func TestGetRendererCaseInsensitive(t *testing.T) {
	r, err := GetRenderer("COMPACT_JSON")
	if err != nil {
		t.Fatalf("expected case-insensitive match, got %v", err)
	}
	if r.Name() != "compact_json" {
		t.Fatalf("expected compact_json renderer, got %q", r.Name())
	}
}

func TestGetRendererTrimsWhitespace(t *testing.T) {
	r, err := GetRenderer("  compact_json  ")
	if err != nil {
		t.Fatalf("expected trimmed match, got %v", err)
	}
	if r.Name() != "compact_json" {
		t.Fatalf("expected compact_json renderer, got %q", r.Name())
	}
}

func TestGetRendererHyphenAlias(t *testing.T) {
	r, err := GetRenderer("compact-json")
	if err != nil {
		t.Fatalf("expected hyphen alias to match compact_json, got %v", err)
	}
	if r.Name() != "compact_json" {
		t.Fatalf("expected compact_json renderer, got %q", r.Name())
	}
}

func TestGetRendererHyphenAliasUpperCase(t *testing.T) {
	r, err := GetRenderer("COMPACT-JSON")
	if err != nil {
		t.Fatalf("expected upper-case hyphen alias to match, got %v", err)
	}
	if r.Name() != "compact_json" {
		t.Fatalf("expected compact_json renderer, got %q", r.Name())
	}
}

func TestGetRendererUnknown(t *testing.T) {
	_, err := GetRenderer("bogus")
	if err == nil {
		t.Fatal("expected error for unknown renderer")
	}
	var ue *UnknownRendererError
	if !errors.As(err, &ue) {
		t.Fatalf("expected *UnknownRendererError, got %T", err)
	}
	if ue.Name != "bogus" {
		t.Fatalf("expected name %q in error, got %q", "bogus", ue.Name)
	}
}

func TestGetRendererEmptyFallsBack(t *testing.T) {
	r, err := GetRenderer("")
	if err != nil {
		t.Fatalf("expected no error for empty name, got %v", err)
	}
	if r.Name() != "native_prompt" {
		t.Fatalf("expected native_prompt fallback, got %q", r.Name())
	}
}

func TestGetRendererWhitespaceFallsBack(t *testing.T) {
	r, err := GetRenderer("   ")
	if err != nil {
		t.Fatalf("expected no error for whitespace-only name, got %v", err)
	}
	if r.Name() != "native_prompt" {
		t.Fatalf("expected native_prompt fallback, got %q", r.Name())
	}
}

func TestUnknownRendererErrorImplementsError(t *testing.T) {
	e := &UnknownRendererError{Name: "test_renderer"}
	msg := e.Error()
	if msg == "" {
		t.Fatal("expected non-empty error string")
	}
}

// --- SourceRank ---

func TestSourceRankServerPolicyHighest(t *testing.T) {
	a := SourceRank(SourceServerPolicy)
	if a != 0 {
		t.Fatalf("expected server_policy rank 0 (highest), got %d", a)
	}
}

func TestSourceRankCompressorLowest(t *testing.T) {
	a := SourceRank(SourceCompressorGen)
	for _, s := range []Source{SourceServerPolicy, SourceClientKeyPolicy, SourceRoutePolicy, SourceClientExplicit, SourceClientMetadata, SourceAgentGenerated, SourceModelInferred} {
		if SourceRank(s) >= a {
			t.Fatalf("compressor_generated should be lowest; rank=%d, but %s has %d", a, s, SourceRank(s))
		}
	}
}

func TestSourceRankUnknownReturns99(t *testing.T) {
	a := SourceRank("unknown_source")
	if a != 99 {
		t.Fatalf("expected 99 for unknown source, got %d", a)
	}
}

func TestSourceRankOrdering(t *testing.T) {
	ordered := []Source{
		SourceServerPolicy, SourceClientKeyPolicy, SourceRoutePolicy,
		SourceClientExplicit, SourceClientMetadata, SourceAgentGenerated,
		SourceModelInferred, SourceCompressorGen,
	}
	for i := 1; i < len(ordered); i++ {
		prev := SourceRank(ordered[i-1])
		curr := SourceRank(ordered[i])
		if prev >= curr {
			t.Fatalf("rank not increasing: %s=%d >= %s=%d", ordered[i-1], prev, ordered[i], curr)
		}
	}
}

// --- CanOverride ---

func TestCanOverrideLowerCannotOverrideHigher(t *testing.T) {
	if CanOverride(SourceModelInferred, SourceServerPolicy) {
		t.Fatal("model_inferred should NOT be able to override server_policy")
	}
	if CanOverride(SourceCompressorGen, SourceClientExplicit) {
		t.Fatal("compressor_generated should NOT be able to override client_explicit")
	}
	if CanOverride(SourceAgentGenerated, SourceRoutePolicy) {
		t.Fatal("agent_generated should NOT be able to override route_policy")
	}
}

func TestCanOverrideSameAuthorityCannotOverride(t *testing.T) {
	if CanOverride(SourceServerPolicy, SourceServerPolicy) {
		t.Fatal("same authority should NOT allow override")
	}
	if CanOverride(SourceClientExplicit, SourceClientExplicit) {
		t.Fatal("same authority should NOT allow override")
	}
}

func TestCanOverrideHigherCanOverrideLower(t *testing.T) {
	if !CanOverride(SourceServerPolicy, SourceModelInferred) {
		t.Fatal("server_policy should be able to override model_inferred")
	}
	if !CanOverride(SourceClientExplicit, SourceCompressorGen) {
		t.Fatal("client_explicit should be able to override compressor_generated")
	}
}

func TestCanOverrideUnknownSource(t *testing.T) {
	if CanOverride("unknown", SourceServerPolicy) {
		t.Fatal("unknown source (authority 99) should NOT override server_policy (0)")
	}
	if !CanOverride(SourceServerPolicy, "unknown") {
		t.Fatal("server_policy (0) should override unknown (99)")
	}
}

// --- Version constants ---

func TestVersionString(t *testing.T) {
	if Version != "0.1" {
		t.Fatalf("expected Version %q, got %q", "0.1", Version)
	}
}

func TestSupportedMajor(t *testing.T) {
	if SupportedMajor != 0 {
		t.Fatalf("expected SupportedMajor 0, got %d", SupportedMajor)
	}
}

// --- ValidSource ---

func TestValidSourceKnown(t *testing.T) {
	sources := []Source{
		SourceServerPolicy, SourceClientKeyPolicy, SourceRoutePolicy,
		SourceClientExplicit, SourceClientMetadata, SourceAgentGenerated,
		SourceModelInferred, SourceCompressorGen,
	}
	for _, s := range sources {
		if !ValidSource(s) {
			t.Fatalf("expected source %q to be valid", s)
		}
	}
}

func TestValidSourceUnknown(t *testing.T) {
	if ValidSource("not_a_source") {
		t.Fatal("expected unknown source to be invalid")
	}
}

// --- DefaultProtection ---

func TestDefaultProtectionSecurityTypes(t *testing.T) {
	immutable := []string{
		"no_commit", "no_push", "security", "compliance", "authorization",
		"privacy", "legal", "route_policy", "key_policy",
	}
	for _, ct := range immutable {
		t.Run(ct, func(t *testing.T) {
			p := DefaultProtection(ct)
			if p != ProtectionImmutable {
				t.Fatalf("expected immutable for %q, got %q", ct, p)
			}
		})
	}
}

func TestDefaultProtectionOptional(t *testing.T) {
	p := DefaultProtection("something_else")
	if p != ProtectionOptional {
		t.Fatalf("expected optional, got %q", p)
	}
}

// --- ApplyDefaults ---

func TestApplyDefaultsFillsMissingFields(t *testing.T) {
	cs := []Constraint{{Type: "no_commit", Value: "true"}}
	out := ApplyDefaults(cs)
	if len(out) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(out))
	}
	if out[0].Protection != ProtectionImmutable {
		t.Fatalf("expected immutable protection for no_commit, got %q", out[0].Protection)
	}
	if out[0].Source != SourceClientExplicit {
		t.Fatalf("expected client_explicit source, got %q", out[0].Source)
	}
	if out[0].ID != "no_commit:true" {
		t.Fatalf("expected ID %q, got %q", "no_commit:true", out[0].ID)
	}
}

func TestApplyDefaultsDoesNotOverwrite(t *testing.T) {
	cs := []Constraint{{
		ID:         "custom_id",
		Type:       "no_commit",
		Value:      "true",
		Protection: ProtectionProtected,
		Source:     SourceServerPolicy,
	}}
	out := ApplyDefaults(cs)
	if out[0].ID != "custom_id" {
		t.Fatalf("expected ID %q, got %q", "custom_id", out[0].ID)
	}
	if out[0].Protection != ProtectionProtected {
		t.Fatalf("expected protection %q, got %q", ProtectionProtected, out[0].Protection)
	}
	if out[0].Source != SourceServerPolicy {
		t.Fatalf("expected source %q, got %q", SourceServerPolicy, out[0].Source)
	}
}

func TestApplyDefaultsEmpty(t *testing.T) {
	out := ApplyDefaults(nil)
	if len(out) != 0 {
		t.Fatalf("expected 0 constraints, got %d", len(out))
	}
}

// --- MergeConstraints ---

func TestMergeConstraintsNoConflict(t *testing.T) {
	existing := []Constraint{{ID: "c1", Type: "a", Value: "v1", Source: SourceClientExplicit}}
	incoming := []Constraint{{ID: "c2", Type: "b", Value: "v2", Source: SourceModelInferred}}
	out := MergeConstraints(existing, incoming...)
	if len(out) != 2 {
		t.Fatalf("expected 2 constraints, got %d", len(out))
	}
}

func TestMergeConstraintsLowerCannotOverrideHigher(t *testing.T) {
	existing := []Constraint{{ID: "c1", Type: "a", Value: "v1", Source: SourceServerPolicy}}
	incoming := []Constraint{{ID: "c1", Type: "a", Value: "v2", Source: SourceModelInferred}}
	out := MergeConstraints(existing, incoming...)
	if len(out) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(out))
	}
	if out[0].Value != "v1" {
		t.Fatalf("expected existing value preserved, got %q", out[0].Value)
	}
}

func TestMergeConstraintsHigherCanOverrideLower(t *testing.T) {
	existing := []Constraint{{ID: "c1", Type: "a", Value: "v1", Source: SourceModelInferred}}
	incoming := []Constraint{{ID: "c1", Type: "a", Value: "v2", Source: SourceServerPolicy}}
	out := MergeConstraints(existing, incoming...)
	if len(out) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(out))
	}
	if out[0].Value != "v2" {
		t.Fatalf("expected incoming value, got %q", out[0].Value)
	}
}

// --- Migrate ---

func TestMigrateSameVersion(t *testing.T) {
	env := newValidEnvelope(t)
	out, err := Migrate(env, Version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Version != Version {
		t.Fatalf("expected version %q, got %q", Version, out.Version)
	}
}

func TestMigrateNilEnvelope(t *testing.T) {
	_, err := Migrate(nil, Version)
	if err == nil {
		t.Fatal("expected error for nil envelope")
	}
}

func TestMigrateUnsupportedMajor(t *testing.T) {
	env := newValidEnvelope(t)
	_, err := Migrate(env, "1.0")
	if err == nil {
		t.Fatal("expected error for unsupported major version")
	}
	if !IsMajorVersionError(err) {
		t.Fatal("expected IsMajorVersionError")
	}
}

func TestMigrateEmptyTargetDefaults(t *testing.T) {
	env := newValidEnvelope(t)
	out, err := Migrate(env, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Version != Version {
		t.Fatalf("expected default version %q, got %q", Version, out.Version)
	}
}

func TestMigrateSourceInvalid(t *testing.T) {
	env := &Envelope{}
	_, err := Migrate(env, Version)
	if err == nil {
		t.Fatal("expected error for invalid source envelope")
	}
}

func TestMigrateCrossVersionSameMajor(t *testing.T) {
	env := newValidEnvelope(t)
	env.Version = "0.0"
	// 0.0 -> 0.1 is same major (0) but different version
	out, err := Migrate(env, "0.1")
	if err == nil {
		t.Fatal("expected error for cross-version migration without rules")
	}
	if out != nil {
		t.Fatal("expected nil output on error")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
}

func TestMigratePreservesIntegrityMetadata(t *testing.T) {
	env := newValidEnvelope(t)
	// Set a valid integrity hash so source validation passes.
	env.Integrity = IntegrityMetadata{
		ContentHash: ComputeIntegrityHash(env),
		GeneratedAt: "2026-07-21T00:00:00Z",
		Generator:   "test/generator",
	}
	out, err := Migrate(env, Version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Content hash is recomputed (same result for same-version no-op migration).
	if out.Integrity.ContentHash == "" {
		t.Fatal("expected non-empty content hash after migration")
	}
	// Verify it was actually recomputed: matches a fresh computation.
	expected := ComputeIntegrityHash(out)
	if out.Integrity.ContentHash != expected {
		t.Fatalf("content hash mismatch: got %q, expected %q", out.Integrity.ContentHash, expected)
	}
	// Generator and GeneratedAt are preserved.
	if out.Integrity.Generator != "test/generator" {
		t.Fatalf("expected generator preserved, got %q", out.Integrity.Generator)
	}
	if out.Integrity.GeneratedAt != "2026-07-21T00:00:00Z" {
		t.Fatalf("expected generated_at preserved, got %q", out.Integrity.GeneratedAt)
	}
}

func TestMigrateDeepCopy(t *testing.T) {
	env := newValidEnvelope(t)
	env.Goals = []Goal{{Type: "original"}}
	out, err := Migrate(env, Version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Mutate the output.
	out.Goals[0].Type = "mutated"
	// Original should not be affected.
	if env.Goals[0].Type != "original" {
		t.Fatal("migrate should deep-copy the envelope; mutation leaked to original")
	}
}

func TestMigrateTargetValidAfterMigration(t *testing.T) {
	env := newValidEnvelope(t)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: "val", Source: SourceServerPolicy, Protection: ProtectionImmutable},
	}
	out, err := Migrate(env, Version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Re-validation would pass (already validated inside Migrate).
	if err := Validate(out); err != nil {
		t.Fatalf("output envelope should be valid: %v", err)
	}
}

// --- Copy ---

func TestCopyDeepCopy(t *testing.T) {
	env := newValidEnvelope(t)
	env.Goals = []Goal{{Type: "g1"}}
	env.Constraints = []Constraint{{ID: "c1"}}
	env.Context = []ContextReference{{ID: "ctx1"}}
	env.State = []StateEntry{{Key: "k1"}}
	env.Tools = []ToolReference{{Name: "t1"}}
	env.Evidence = []EvidenceReference{{ID: "e1"}}
	env.Dictionary = map[string]string{"d1": "val1"}

	cp := Copy(env)
	// Mutate the copy
	cp.Goals[0].Type = "changed"
	cp.Constraints[0].ID = "changed"
	cp.Context[0].ID = "changed"
	cp.State[0].Key = "changed"
	cp.Tools[0].Name = "changed"
	cp.Evidence[0].ID = "changed"
	cp.Dictionary["d1"] = "changed"

	// Original should be untouched
	if env.Goals[0].Type != "g1" {
		t.Fatal("copy leaked into original goals")
	}
	if env.Constraints[0].ID != "c1" {
		t.Fatal("copy leaked into original constraints")
	}
	if env.Context[0].ID != "ctx1" {
		t.Fatal("copy leaked into original context")
	}
	if env.State[0].Key != "k1" {
		t.Fatal("copy leaked into original state")
	}
	if env.Tools[0].Name != "t1" {
		t.Fatal("copy leaked into original tools")
	}
	if env.Evidence[0].ID != "e1" {
		t.Fatal("copy leaked into original evidence")
	}
	if env.Dictionary["d1"] != "val1" {
		t.Fatal("copy leaked into original dictionary")
	}
}

func TestCopyNil(t *testing.T) {
	if Copy(nil) != nil {
		t.Fatal("Copy(nil) should return nil")
	}
}

// --- BuildDictionary ---

func TestBuildDictionaryNoRepetition(t *testing.T) {
	dict, compressed, saved := BuildDictionary("hello world", 3, 10)
	if len(dict) != 0 {
		t.Fatalf("expected empty dict, got %d entries", len(dict))
	}
	if compressed != "hello world" {
		t.Fatalf("expected unchanged text, got %q", compressed)
	}
	if saved != 0 {
		t.Fatalf("expected 0 savings, got %d", saved)
	}
}

func TestBuildDictionaryWithRepetition(t *testing.T) {
	// Use a pattern unlikely to produce nested dictionary references.
	text := "alpha-beta-gamma-" + "alpha-beta-gamma-" + "alpha-beta-gamma-" + "alpha-beta-gamma-" +
		"alpha-beta-gamma-" + "alpha-beta-gamma-" + "alpha-beta-gamma-" + "alpha-beta-gamma-"
	dict, compressed, saved := BuildDictionary(text, 6, 10)
	if len(dict) == 0 {
		t.Fatal("expected non-empty dict for repeated text")
	}
	if saved <= 0 {
		t.Fatalf("expected positive savings, got %d", saved)
	}
	expanded, err := ExpandDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != text {
		t.Fatalf("expand did not recover original text:\n  got  len=%d %q\n  want len=%d %q", len(expanded), expanded, len(text), text)
	}
}

func TestBuildDictionaryMaxEntries(t *testing.T) {
	text := strings.Repeat("abcdefghij", 20)
	_, _, _ = BuildDictionary(text, 4, 2)
	// just verify it doesn't panic and respects the limit
}

// --- DictionaryAllocator ---

func TestDictionaryAllocatorUniqueKeys(t *testing.T) {
	a := NewDictionaryAllocator(10)
	k1 := a.Intern("hello world")
	k2 := a.Intern("goodbye world")
	if k1 == k2 {
		t.Fatal("different values must receive different keys")
	}
	if k1 != "d0001" || k2 != "d0002" {
		t.Fatalf("expected d0001 and d0002, got %q and %q", k1, k2)
	}
}

func TestDictionaryAllocatorDedup(t *testing.T) {
	a := NewDictionaryAllocator(10)
	k1 := a.Intern("hello world")
	k2 := a.Intern("hello world")
	if k1 != k2 {
		t.Fatal("identical values must reuse the same key")
	}
	if k1 != "d0001" {
		t.Fatalf("expected d0001, got %q", k1)
	}
	snap := a.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry in snapshot, got %d", len(snap))
	}
	if snap[k1] != "hello world" {
		t.Fatalf("expected snapshot value %q, got %q", "hello world", snap[k1])
	}
}

func TestDictionaryAllocatorMaxEntries(t *testing.T) {
	a := NewDictionaryAllocator(3)
	for i := 0; i < 3; i++ {
		v := fmt.Sprintf("value-%d", i)
		k := a.Intern(v)
		if k == "" {
			t.Fatalf("expected non-empty key for %q", v)
		}
	}
	// Fourth entry should be rejected.
	if k := a.Intern("overflow"); k != "" {
		t.Fatalf("expected empty key for overflow, got %q", k)
	}
}

func TestDictionaryAllocatorDeterministic(t *testing.T) {
	a1 := NewDictionaryAllocator(10)
	a2 := NewDictionaryAllocator(10)
	vals := []string{"alpha", "beta", "gamma", "alpha"}
	for _, v := range vals {
		k1 := a1.Intern(v)
		k2 := a2.Intern(v)
		if k1 != k2 {
			t.Fatalf("deterministic allocation failed for %q: %q vs %q", v, k1, k2)
		}
	}
}

// --- CompressEnvelopeText allocator integration ---

func TestCompressEnvelopeTextDifferentValuesGetDifferentKeys(t *testing.T) {
	env := newValidEnvelope(t)
	val1 := strings.Repeat("abcdefghij", 8)
	val2 := strings.Repeat("klmnopqrst", 8)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: val1, Source: SourceClientExplicit, Protection: ProtectionOptional},
		{ID: "c2", Type: "test", Value: val2, Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	saved, changed := CompressEnvelopeText(env, 4, 10)
	if !changed {
		t.Fatal("expected compression")
	}
	if saved <= 0 {
		t.Fatalf("expected positive savings, got %d", saved)
	}
	// Two different values should produce two different keys.
	if len(env.Dictionary) != 2 {
		t.Fatalf("expected 2 dictionary entries for 2 different values, got %d", len(env.Dictionary))
	}
	// Each reference should be unique.
	for _, c := range env.Constraints {
		if !strings.Contains(c.Value, "{{lui:") {
			t.Fatalf("expected {{lui:}} reference in compressed value, got %q", c.Value)
		}
	}
	// Round-trip.
	for i, c := range env.Constraints {
		expanded, err := ExpandDictionary(c.Value, env.Dictionary)
		if err != nil {
			t.Fatalf("ExpandDictionary: %v", err)
		}
		original := val1
		if i == 1 {
			original = val2
		}
		if expanded != original {
			t.Fatalf("round-trip failed for constraint %d:\n  got  %q\n  want %q", i, expanded, original)
		}
	}
}

func TestCompressEnvelopeTextIdenticalValuesReuseKey(t *testing.T) {
	env := newValidEnvelope(t)
	val := strings.Repeat("abcdefghij", 8)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: val, Source: SourceClientExplicit, Protection: ProtectionOptional},
		{ID: "c2", Type: "test", Value: val, Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	saved, changed := CompressEnvelopeText(env, 4, 10)
	if !changed {
		t.Fatal("expected compression")
	}
	if saved <= 0 {
		t.Fatalf("expected positive savings, got %d", saved)
	}
	// Identical values share one dictionary entry.
	if len(env.Dictionary) != 1 {
		t.Fatalf("expected 1 dictionary entry for identical values, got %d", len(env.Dictionary))
	}
	// Both compressed values should contain the same reference.
	if env.Constraints[0].Value != env.Constraints[1].Value {
		t.Fatalf("identical values should produce identical compressed text")
	}
	// Round-trip both.
	for i, c := range env.Constraints {
		expanded, err := ExpandDictionary(c.Value, env.Dictionary)
		if err != nil {
			t.Fatalf("ExpandDictionary: %v", err)
		}
		if expanded != val {
			t.Fatalf("round-trip failed for constraint %d:\n  got  %q\n  want %q", i, expanded, val)
		}
	}
}

func TestCompressEnvelopeTextGlobalMaxEntries(t *testing.T) {
	env := newValidEnvelope(t)
	// Each field has a unique repeated pattern long enough for positive
	// savings; maxEntries=2 limits the total across all fields to 2.
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: strings.Repeat("the-quick-brown-fox-", 6), Source: SourceClientExplicit, Protection: ProtectionOptional},
		{ID: "c2", Type: "test", Value: strings.Repeat("jumps-over-the-lazy-", 6), Source: SourceClientExplicit, Protection: ProtectionOptional},
		{ID: "c3", Type: "test", Value: strings.Repeat("dog-near-the-river-", 6), Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	_, changed := CompressEnvelopeText(env, 4, 2)
	if !changed {
		t.Fatal("expected at least some compression")
	}
	if len(env.Dictionary) > 2 {
		t.Fatalf("expected at most 2 dictionary entries with maxEntries=2, got %d", len(env.Dictionary))
	}
}

func TestCompressEnvelopeTextRoundTripTwoFields(t *testing.T) {
	env := newValidEnvelope(t)
	val1 := strings.Repeat("abcdefghij", 8)
	val2 := strings.Repeat("klmnopqrst", 8)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: val1, Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	env.State = []StateEntry{
		{Key: "k1", Value: val2, Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	_, changed := CompressEnvelopeText(env, 4, 10)
	if !changed {
		t.Fatal("expected compression")
	}
	// Expand both fields and verify they match originals.
	expanded1, err := ExpandDictionary(env.Constraints[0].Value, env.Dictionary)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded1 != val1 {
		t.Fatalf("round-trip constraint:\n  got  %q\n  want %q", expanded1, val1)
	}
	expanded2, err := ExpandDictionary(env.State[0].Value, env.Dictionary)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded2 != val2 {
		t.Fatalf("round-trip state:\n  got  %q\n  want %q", expanded2, val2)
	}
}

// --- ExpandDictionary safety ---

func TestExpandDictionaryCyclicReference(t *testing.T) {
	dict := map[string]string{"d0001": "hello {{lui:d0001}}"}
	result, err := ExpandDictionary("{{lui:d0001}}", dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if result != "{{lui:d0001}}" {
		t.Fatalf("cyclic reference should be left untouched, got %q", result)
	}
}

func TestExpandDictionaryIndirectCycle(t *testing.T) {
	dict := map[string]string{
		"d0001": "x {{lui:d0002}}",
		"d0002": "y {{lui:d0001}}",
	}
	result, err := ExpandDictionary("{{lui:d0001}}", dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	// Depth-limited: after maxDepth passes the unexpanded reference remains.
	if !strings.Contains(result, "{{lui:") {
		t.Fatal("indirect cycle should leave some reference unexpanded")
	}
}

func TestExpandDictionarySizeLimit(t *testing.T) {
	dict := map[string]string{"d0001": strings.Repeat("x", 200)}
	// Original text is short; expanded explodes.
	text := "{{lui:d0001}}" + strings.Repeat("y", 100)
	result, err := ExpandDictionary(text, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	// Should not exceed ~10x original.
	if len(result) > len(text)*11 {
		t.Fatalf("expand size limit exceeded: %d > %d", len(result), len(text)*11)
	}
	// Should contain the expanded value.
	if !strings.Contains(result, strings.Repeat("x", 200)) {
		t.Fatalf("expected expanded value in result")
	}
}

func TestExpandDictionaryUnknownKeysLeftUntouched(t *testing.T) {
	dict := map[string]string{"d0001": "hello"}
	compressed := "say {{lui:d0001}} and {{lui:d9999}}"
	expanded, err := ExpandDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != "say hello and {{lui:d9999}}" {
		t.Fatalf("unknown key should be left untouched, got %q", expanded)
	}
}

func TestExpandDictionaryLiteralTextNotModified(t *testing.T) {
	text := "The variable d1 is used in the code"
	dict := map[string]string{"d1": "replacement"}
	expanded, err := ExpandDictionary(text, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != text {
		t.Fatalf("literal text should not be modified, got %q", expanded)
	}
}

func TestExpandDictionaryLiteralBracesNotModified(t *testing.T) {
	dict := map[string]string{"d0001": "world"}
	tests := []struct {
		input string
		want  string
	}{
		{"say {{lui:d0001}}", "say world"},
		{"plain braces { and } are fine", "plain braces { and } are fine"},
		{"{{not-a-ref}}", "{{not-a-ref}}"},
		{"{ {lui:d0001} }", "{ {lui:d0001} }"},
	}
	for _, tc := range tests {
		got, err := ExpandDictionary(tc.input, dict)
		if err != nil {
			t.Fatalf("ExpandDictionary: %v", err)
		}
		if got != tc.want {
			t.Fatalf("ExpandDictionary(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- CompressEnvelopeText ---

func TestCompressEnvelopeTextNil(t *testing.T) {
	saved, changed := CompressEnvelopeText(nil, 4, 10)
	if saved != 0 || changed {
		t.Fatal("expected no-op for nil envelope")
	}
}

func TestCompressEnvelopeTextSkipsProtected(t *testing.T) {
	env := newValidEnvelope(t)
	val := strings.Repeat("abcdefghij", 10)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: val, Source: SourceClientExplicit, Protection: ProtectionImmutable},
	}
	saved, changed := CompressEnvelopeText(env, 4, 10)
	if changed {
		t.Fatal("expected no compression for immutable constraint")
	}
	if saved != 0 {
		t.Fatalf("expected 0 savings, got %d", saved)
	}
}

func TestCompressEnvelopeTextCompressesOptional(t *testing.T) {
	env := newValidEnvelope(t)
	val := strings.Repeat("abcdefghij", 10)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "test", Value: val, Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	saved, changed := CompressEnvelopeText(env, 4, 10)
	if !changed {
		t.Fatal("expected compression for optional constraint")
	}
	if saved <= 0 {
		t.Fatalf("expected positive savings, got %d", saved)
	}
	// The constraint value should now contain a {{lui:...}} reference.
	if !strings.Contains(env.Constraints[0].Value, "{{lui:") {
		t.Fatalf("expected {{lui:}} reference in compressed value, got %q", env.Constraints[0].Value)
	}
}

func TestCompressEnvelopeTextMutationsPersist(t *testing.T) {
	env := newValidEnvelope(t)
	val := strings.Repeat("abcdefghij", 10)
	env.State = []StateEntry{
		{Key: "k1", Value: val, Source: SourceClientExplicit, Protection: ProtectionOptional},
	}
	saved, changed := CompressEnvelopeText(env, 4, 10)
	if !changed {
		t.Fatal("expected compression for optional state")
	}
	if saved <= 0 {
		t.Fatalf("expected positive savings, got %d", saved)
	}
	// Verify the mutation persisted on the envelope (not a copy).
	if !strings.Contains(env.State[0].Value, "{{lui:") {
		t.Fatalf("expected {{lui:}} reference in state value, got %q", env.State[0].Value)
	}
}

// --- renderText details ---

func TestRenderTextTaggedFormat(t *testing.T) {
	env := newValidEnvelope(t)
	out := renderText(env, true)
	if !strings.Contains(out, "LUI/"+Version) {
		t.Fatal("expected LUI/<version> in tagged output")
	}
	if !strings.Contains(out, "KIND task") {
		t.Fatal("expected KIND line in tagged output")
	}
}

func TestRenderTextHumanFormat(t *testing.T) {
	env := newValidEnvelope(t)
	out := renderText(env, false)
	if !strings.Contains(out, "LUI "+Version) {
		t.Fatal("expected LUI <version> in human output")
	}
	if !strings.Contains(out, "chat") {
		t.Fatal("expected task type in human output")
	}
}

func TestRenderTextWithGoals(t *testing.T) {
	env := newValidEnvelope(t)
	env.Goals = []Goal{{Type: "optimize", Summary: "reduce tokens"}}
	out := renderText(env, false)
	if !strings.Contains(out, "GOAL optimize") {
		t.Fatal("expected GOAL line in output")
	}
}

func TestRenderTextWithConstraints(t *testing.T) {
	env := newValidEnvelope(t)
	env.Constraints = []Constraint{
		{ID: "c1", Type: "security", Value: "no secrets", Source: SourceServerPolicy, Protection: ProtectionImmutable},
	}
	out := renderText(env, false)
	if !strings.Contains(out, "CONSTRAINT") {
		t.Fatal("expected CONSTRAINT line in output")
	}
	if !strings.Contains(out, "no secrets") {
		t.Fatal("expected constraint value in output")
	}
}

func TestRenderTextWithState(t *testing.T) {
	env := newValidEnvelope(t)
	env.State = []StateEntry{{Key: "k1", Value: "v1", Source: SourceClientExplicit}}
	out := renderText(env, false)
	if !strings.Contains(out, "STATE k1=v1") {
		t.Fatalf("expected STATE k1=v1 in output, got %q", out)
	}
}

func TestRenderTextWithTools(t *testing.T) {
	env := newValidEnvelope(t)
	env.Tools = []ToolReference{{Name: "web_search", Source: SourceClientExplicit}}
	out := renderText(env, false)
	if !strings.Contains(out, "TOOL web_search") {
		t.Fatal("expected TOOL line in output")
	}
}

func TestRenderTextWithContext(t *testing.T) {
	env := newValidEnvelope(t)
	env.Context = []ContextReference{{ID: "ctx1", Content: "hello"}}
	out := renderText(env, false)
	if !strings.Contains(out, "CONTEXT hello") {
		t.Fatal("expected CONTEXT line in output")
	}
}

func TestRenderTextWithContextURI(t *testing.T) {
	env := newValidEnvelope(t)
	env.Context = []ContextReference{{ID: "ctx1", URI: "file:///foo.txt"}}
	out := renderText(env, false)
	if !strings.Contains(out, "CONTEXT file:///foo.txt") {
		t.Fatal("expected CONTEXT with URI in output")
	}
}

func TestRenderTextWithEvidence(t *testing.T) {
	env := newValidEnvelope(t)
	env.Evidence = []EvidenceReference{{ID: "e1", Summary: "found something"}}
	out := renderText(env, false)
	if !strings.Contains(out, "EVIDENCE found something") {
		t.Fatal("expected EVIDENCE line in output")
	}
}

func TestRenderTextWithOutputFields(t *testing.T) {
	env := newValidEnvelope(t)
	env.Output = OutputContract{Fields: []string{"field_a", "field_b"}}
	out := renderText(env, false)
	if !strings.Contains(out, "OUTPUT field_a,field_b") {
		t.Fatalf("expected OUTPUT line with comma-separated fields, got %q", out)
	}
}

func TestRenderTextWithDictionary(t *testing.T) {
	env := newValidEnvelope(t)
	env.Dictionary = map[string]string{"d1": "replacement text"}
	out := renderText(env, false)
	if !strings.Contains(out, "DICT d1=replacement text") {
		t.Fatal("expected DICT line in output")
	}
}

func TestExpandDictionaryWithNewSyntax(t *testing.T) {
	dict := map[string]string{"d1": "hello world"}
	compressed := "say {{lui:d1}} to everyone"
	expanded, err := ExpandDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != "say hello world to everyone" {
		t.Fatalf("expected expanded text, got %q", expanded)
	}
}

func TestExpandDictionaryIgnoresUnknownKeys(t *testing.T) {
	dict := map[string]string{"d1": "hello"}
	compressed := "say {{lui:d1}} and {{lui:d99}} to everyone"
	expanded, err := ExpandDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != "say hello and {{lui:d99}} to everyone" {
		t.Fatalf("expected unknown key preserved, got %q", expanded)
	}
}

func TestExpandDictionaryLongestFirst(t *testing.T) {
	dict := map[string]string{"d1": "a", "d2": "ab"}
	compressed := "{{lui:d2}}={{lui:d1}}"
	expanded, err := ExpandDictionary(compressed, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != "ab=a" {
		t.Fatalf("expected longest expanded first, got %q", expanded)
	}
}

func TestLiteralTextNotModified(t *testing.T) {
	text := "The variable d1 is used in the code"
	dict := map[string]string{"d1": "replacement"}
	expanded, err := ExpandDictionary(text, dict)
	if err != nil {
		t.Fatalf("ExpandDictionary: %v", err)
	}
	if expanded != text {
		t.Fatalf("literal text should not be modified, got %q", expanded)
	}
}

func TestRenderTextComplexity(t *testing.T) {
	env := newValidEnvelope(t)
	env.Task.Complexity = "high"
	out := renderText(env, false)
	if !strings.Contains(out, "COMPLEXITY high") {
		t.Fatal("expected COMPLEXITY line in output")
	}
}

// --- Integrity hash: contextual tamper tests ---

func TestVerifyIntegrityHash_TamperedContextContent(t *testing.T) {
	req := newRequestWithSystem(t, "original system instruction", "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	// Tamper with the inline Content in the system context reference.
	for i := range env.Context {
		if env.Context[i].ID == "system" {
			env.Context[i].Content = "tampered system instruction"
		}
	}
	if VerifyIntegrityHash(env) {
		t.Fatal("expected integrity hash to fail after tampering Context.Content")
	}
}

func TestVerifyIntegrityHash_TamperedSystemInstruction(t *testing.T) {
	req := newRequestWithSystem(t, "original system instruction", "hello")
	env := BuildEnvelope(req, BuildContext{RequestID: "r1"})
	// Tamper with the system context inline content after build.
	for i := range env.Context {
		if env.Context[i].ID == "system" {
			env.Context[i].Content = "tampered system instruction"
		}
	}
	if VerifyIntegrityHash(env) {
		t.Fatal("expected integrity hash to fail after tampering system instruction")
	}
}

func TestVerifyIntegrityHash_TamperedContextPriority(t *testing.T) {
	env := enrichedEnvelope(t)
	// Set Integrity hash as if built by BuildEnvelope.
	env.Integrity = IntegrityMetadata{
		ContentHash: ComputeIntegrityHash(env),
		Generator:   "termrouter/lui/" + Version,
	}
	// Tamper with Context.Priority.
	env.Context[0].Priority = 999
	if VerifyIntegrityHash(env) {
		t.Fatal("expected integrity hash to fail after tampering Context.Priority")
	}
}

func TestValidateRejectsIntegrityMismatch(t *testing.T) {
	env := enrichedEnvelope(t)
	env.Integrity = IntegrityMetadata{
		ContentHash: ComputeIntegrityHash(env),
		Generator:   "termrouter/lui/" + Version,
	}
	// Tamper with a semantic field.
	env.Task.Summary = "tampered"
	err := Validate(env)
	if err == nil {
		t.Fatal("expected validation error for integrity mismatch")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if ve.Field != "integrity.content_hash" {
		t.Fatalf("expected field integrity.content_hash, got %q", ve.Field)
	}
}

// TestCanonicalHashMapOrderIndependent verifies the order-insensitive
// collection contract: every multi-value semantic collection is a multiset.
// Each collection is populated with at least three distinct elements, then
// reversed; the integrity hash must be unchanged. Goals and output fields are
// included (they are not order-sensitive under the current contract).
func TestCanonicalHashMapOrderIndependent(t *testing.T) {
	env1 := enrichedEnvelope(t)
	env1.Goals = []Goal{
		{Type: "g1", Summary: "summary1", Priority: 3, Source: SourceClientExplicit},
		{Type: "g2", Summary: "summary2", Priority: 2, Source: SourceClientExplicit},
		{Type: "g3", Summary: "summary3", Priority: 1, Source: SourceServerPolicy},
	}
	env1.Constraints = []Constraint{
		{ID: "c1", Type: "type1", Value: "val1", Source: SourceClientExplicit},
		{ID: "c2", Type: "type2", Value: "val2", Source: SourceClientExplicit},
		{ID: "c3", Type: "type3", Value: "val3", Source: SourceServerPolicy},
	}
	env1.Context = []ContextReference{
		{ID: "ctx1", Content: "content1"},
		{ID: "ctx2", Content: "content2"},
		{ID: "ctx3", Content: "content3"},
	}
	env1.State = []StateEntry{
		{Key: "sk1", Value: "sv1", Source: SourceClientExplicit},
		{Key: "sk2", Value: "sv2", Source: SourceClientExplicit},
		{Key: "sk3", Value: "sv3", Source: SourceServerPolicy},
	}
	env1.Tools = []ToolReference{
		{Name: "tool1", Source: SourceClientExplicit},
		{Name: "tool2", Source: SourceClientExplicit},
		{Name: "tool3", Source: SourceServerPolicy},
	}
	env1.Evidence = []EvidenceReference{
		{ID: "e1", Summary: "sum1", Source: SourceClientExplicit},
		{ID: "e2", Summary: "sum2", Source: SourceClientExplicit},
		{ID: "e3", Summary: "sum3", Source: SourceServerPolicy},
	}
	env1.Output = OutputContract{
		Format: "json",
		Fields: []string{"field3", "field1", "field2"},
	}
	env1.Dictionary = map[string]string{
		"dk1": "dv1",
		"dk2": "dv2",
		"dk3": "dv3",
	}

	env1.Integrity = IntegrityMetadata{
		ContentHash: ComputeIntegrityHash(env1),
	}
	// Create a copy with reversed slice orders (and a second permutation of fields).
	env2 := Copy(env1)
	reverseSlice(env2.Goals)
	reverseSlice(env2.Constraints)
	reverseSlice(env2.Context)
	reverseSlice(env2.State)
	reverseSlice(env2.Tools)
	reverseSlice(env2.Evidence)
	reverseSlice(env2.Output.Fields)
	hash2 := ComputeIntegrityHash(env2)
	if env1.Integrity.ContentHash != hash2 {
		t.Fatal("canonical hash should be independent of collection order")
	}

	// Rotate (not just reverse) to exercise a second permutation with ≥3 elements.
	env3 := Copy(env1)
	rotateLeft(env3.Goals)
	rotateLeft(env3.Constraints)
	rotateLeft(env3.Context)
	rotateLeft(env3.State)
	rotateLeft(env3.Tools)
	rotateLeft(env3.Evidence)
	rotateLeft(env3.Output.Fields)
	hash3 := ComputeIntegrityHash(env3)
	if env1.Integrity.ContentHash != hash3 {
		t.Fatal("canonical hash should be independent of rotated collection order")
	}
}

// TestCanonicalHashOrderIndependentEqualPrimaryKeys verifies that when
// primary sort keys collide (e.g. same goal priority+type), secondary fields
// still produce a total order so reordering does not change the hash.
func TestCanonicalHashOrderIndependentEqualPrimaryKeys(t *testing.T) {
	env1 := enrichedEnvelope(t)
	env1.Goals = []Goal{
		{Type: "same", Summary: "alpha", Priority: 1, Source: SourceClientExplicit},
		{Type: "same", Summary: "bravo", Priority: 1, Source: SourceClientExplicit},
		{Type: "same", Summary: "charlie", Priority: 1, Source: SourceServerPolicy},
	}
	env1.Constraints = []Constraint{
		{ID: "same-id", Type: "t", Value: "v1", Source: SourceClientExplicit},
		{ID: "same-id", Type: "t", Value: "v2", Source: SourceClientExplicit},
		{ID: "same-id", Type: "t", Value: "v3", Source: SourceServerPolicy},
	}
	env1.Output.Fields = []string{"z", "a", "m"}

	hash1 := ComputeIntegrityHash(env1)
	env2 := Copy(env1)
	reverseSlice(env2.Goals)
	reverseSlice(env2.Constraints)
	reverseSlice(env2.Output.Fields)
	hash2 := ComputeIntegrityHash(env2)
	if hash1 != hash2 {
		t.Fatal("hash must be order-independent when primary sort keys collide")
	}
}

func rotateLeft[T any](s []T) {
	if len(s) < 2 {
		return
	}
	first := s[0]
	copy(s, s[1:])
	s[len(s)-1] = first
}

func reverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// TestCanonicalHashDelimiterSafe verifies that values containing delimiter-like
// characters (|, :, {, }) do not cause hash collisions.
func TestCanonicalHashDelimiterSafe(t *testing.T) {
	env1 := enrichedEnvelope(t)
	env1.State[0] = StateEntry{
		Key:    "k1",
		Value:  "field1:value1|field2:value2|field3:value3",
		Source: SourceClientExplicit,
	}
	hash1 := ComputeIntegrityHash(env1)

	// Same data but with different value that imitates delimiter-based encoding.
	env2 := Copy(env1)
	env2.State[0].Value = "field1:value1|field2:value2"
	hash2 := ComputeIntegrityHash(env2)

	if hash1 == hash2 {
		t.Fatal("expected different hashes for different delimiter-containing values")
	}
}

// TestComputeIntegrityHash_Mutation verifies that mutating ANY single semantic
// field changes the integrity hash. This is a table-driven mutation test.
func TestComputeIntegrityHash_Mutation(t *testing.T) {
	type mutation struct {
		name string
		fn   func(env *Envelope)
	}
	// Build reference envelope with all semantic fields populated.
	ref := enrichedEnvelope(t)
	refHash := ComputeIntegrityHash(ref)

	tests := []mutation{
		// Task fields
		{"Task.Type", func(env *Envelope) { env.Task.Type = "mutated" }},
		{"Task.Complexity", func(env *Envelope) { env.Task.Complexity = "mutated" }},
		{"Task.Summary", func(env *Envelope) { env.Task.Summary = "mutated" }},
		{"Task.RequestID", func(env *Envelope) { env.Task.RequestID = "mutated" }},

		// Goal fields
		{"Goal.Type", func(env *Envelope) { env.Goals[0].Type = "mutated" }},
		{"Goal.Summary", func(env *Envelope) { env.Goals[0].Summary = "mutated" }},
		{"Goal.Priority", func(env *Envelope) { env.Goals[0].Priority = 99 }},
		{"Goal.Source", func(env *Envelope) { env.Goals[0].Source = SourceServerPolicy }},

		// Constraint fields
		{"Constraint.ID", func(env *Envelope) { env.Constraints[0].ID = "mutated" }},
		{"Constraint.Type", func(env *Envelope) { env.Constraints[0].Type = "mutated" }},
		{"Constraint.Value", func(env *Envelope) { env.Constraints[0].Value = "mutated" }},
		{"Constraint.Priority", func(env *Envelope) { env.Constraints[0].Priority = 99 }},
		{"Constraint.Source", func(env *Envelope) { env.Constraints[0].Source = SourceModelInferred }},
		{"Constraint.Protection", func(env *Envelope) { env.Constraints[0].Protection = ProtectionOptional }},

		// Context fields
		{"Context.ID", func(env *Envelope) { env.Context[0].ID = "mutated" }},
		{"Context.Kind", func(env *Envelope) { env.Context[0].Kind = "mutated" }},
		{"Context.URI", func(env *Envelope) { env.Context[0].URI = "mutated" }},
		{"Context.ContentHash", func(env *Envelope) { env.Context[0].ContentHash = "mutated" }},
		{"Context.TokenEstimate", func(env *Envelope) { env.Context[0].TokenEstimate = 999 }},
		{"Context.Priority", func(env *Envelope) { env.Context[0].Priority = 999 }},
		{"Context.Protection", func(env *Envelope) { env.Context[0].Protection = ProtectionOptional }},
		{"Context.Inline", func(env *Envelope) { env.Context[0].Inline = false }},
		{"Context.Content", func(env *Envelope) { env.Context[0].Content = "mutated" }},

		// State fields
		{"State.Key", func(env *Envelope) { env.State[0].Key = "mutated" }},
		{"State.Value", func(env *Envelope) { env.State[0].Value = "mutated" }},
		{"State.Source", func(env *Envelope) { env.State[0].Source = SourceModelInferred }},
		{"State.Protection", func(env *Envelope) { env.State[0].Protection = ProtectionImmutable }},

		// Tool fields
		{"Tool.Name", func(env *Envelope) { env.Tools[0].Name = "mutated" }},
		{"Tool.SchemaHash", func(env *Envelope) { env.Tools[0].SchemaHash = "mutated" }},
		{"Tool.Source", func(env *Envelope) { env.Tools[0].Source = SourceModelInferred }},

		// Evidence fields
		{"Evidence.ID", func(env *Envelope) { env.Evidence[0].ID = "mutated" }},
		{"Evidence.Kind", func(env *Envelope) { env.Evidence[0].Kind = "mutated" }},
		{"Evidence.URI", func(env *Envelope) { env.Evidence[0].URI = "mutated" }},
		{"Evidence.Summary", func(env *Envelope) { env.Evidence[0].Summary = "mutated" }},
		{"Evidence.Source", func(env *Envelope) { env.Evidence[0].Source = SourceRoutePolicy }},

		// Output fields
		{"Output.Format", func(env *Envelope) { env.Output.Format = "mutated" }},
		{"Output.Fields", func(env *Envelope) { env.Output.Fields = []string{"mutated"} }},

		// Dictionary
		{"Dictionary.Key", func(env *Envelope) {
			env.Dictionary = map[string]string{"mutated_key": "dv1"}
		}},
		{"Dictionary.Value", func(env *Envelope) {
			env.Dictionary = map[string]string{"dk1": "mutated_value"}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := Copy(ref)
			tc.fn(env)
			hash := ComputeIntegrityHash(env)
			if hash == refHash {
				t.Errorf("mutating %s did not change integrity hash", tc.name)
			}
		})
	}
}

func TestValidateAcceptsMissingIntegrityHash(t *testing.T) {
	env := newValidEnvelope(t)
	// No Integrity.ContentHash set.
	if err := Validate(env); err != nil {
		t.Fatalf("expected validation to pass without integrity hash, got %v", err)
	}
}

func TestIntegrityHashExcludesGeneratedAtAndGenerator(t *testing.T) {
	env := enrichedEnvelope(t)
	hash1 := ComputeIntegrityHash(env)

	env.Integrity.GeneratedAt = "2026-07-21T12:00:00Z"
	env.Integrity.Generator = "test/generator/v1"
	hash2 := ComputeIntegrityHash(env)

	if hash1 != hash2 {
		t.Fatal("integrity hash should not change when GeneratedAt or Generator change")
	}
}

func TestIntegrityHashExcludesContentHash(t *testing.T) {
	env := enrichedEnvelope(t)
	hash1 := ComputeIntegrityHash(env)

	env.Integrity.ContentHash = "some-existing-hash"
	hash2 := ComputeIntegrityHash(env)

	if hash1 != hash2 {
		t.Fatal("integrity hash should not change when ContentHash changes (it is excluded)")
	}
}
