package optimization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
)

// --- fixtures ---

func testOptConfig(enabled bool, mode config.OptimizationMode) config.OptimizationConfig {
	cfg := config.DefaultOptimization()
	cfg.Enabled = enabled
	cfg.DefaultMode = mode
	cfg.Deterministic.StripANSI = true
	cfg.Deterministic.CompactJSON = true
	cfg.Deterministic.Deduplicate = true
	cfg.Deterministic.CompactLogs = false
	cfg.Conversation.Enabled = false
	cfg.SemanticCompression.Enabled = false
	cfg.Output.Mode = "off"
	return cfg
}

func testOptConfigAggressive() config.OptimizationConfig {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = true
	cfg.Deterministic.CompactLogs = true
	cfg.Deterministic.Deduplicate = false
	return cfg
}

func simpleRequest() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{
		System: "You are a helpful assistant.",
		Messages: []normalization.Message{
			{
				Role:    normalization.RoleUser,
				Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hello world"}},
			},
		},
	}
}

func requestWithTools() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{
		System: "System prompt",
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hello"}}},
		},
		Tools: []normalization.Tool{
			{Name: "alpha", Description: "tool alpha", InputSchema: map[string]any{"type": "object"}},
			{Name: "beta", Description: "tool beta", InputSchema: map[string]any{"type": "object"}},
		},
	}
}

func requestWithToolResults() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "do stuff"}}},
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "result at src/main.go:42"}}},
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "result at src/main.go:42"}}},
		},
	}
}

func requestWithANSI() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: "\x1b[31mred\x1b[0m and normal"},
			}},
		},
	}
}

func requestWithJSONToolResult() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: `{"key": "value", "nested": {"a": 1,  "b": 2}}`},
			}},
		},
	}
}

func requestWithDuplicateToolResults() *normalization.NormalizedRequest {
	dup := `{"output":"some very long repeated content that should trigger deduplication threshold and be replaced"}` +
		` some padding to make this longer than 64 chars total so the dedupe optimizer will catch it`
	return &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: dup},
			}},
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: dup},
			}},
		},
	}
}

func requestWithLogs() *normalization.NormalizedRequest {
	logs := "INFO starting\nINFO starting\nINFO starting\nINFO processing\nERROR bad input\nINFO processing"
	return &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: logs},
			}},
		},
	}
}

func defaultOC() OptimizationContext {
	return OptimizationContext{
		RequestID:  "req-1",
		ProviderID: "openai",
		ModelID:    "gpt-4",
	}
}

func nullLogger() Logger { return nil }

// --- ResolveMode tests ---

func TestResolveMode_ServerDefaultSafe(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	req, app, err := ResolveMode(cfg, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeSafe {
		t.Fatalf("expected requested safe, got %v", req)
	}
	if app != config.OptModeSafe {
		t.Fatalf("expected applied safe, got %v", app)
	}
}

func TestResolveMode_ServerDefaultBalanced(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	req, app, err := ResolveMode(cfg, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeBalanced {
		t.Fatalf("expected requested balanced, got %v", req)
	}
	if app != config.OptModeBalanced {
		t.Fatalf("expected applied balanced, got %v", app)
	}
}

func TestResolveMode_ClientPreferenceOverrides(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	req, app, err := ResolveMode(cfg, "balanced", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeBalanced {
		t.Fatalf("expected requested balanced, got %v", req)
	}
	if app != config.OptModeBalanced {
		t.Fatalf("expected applied balanced, got %v", app)
	}
}

func TestResolveMode_ClientPreferenceClampedByServer(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	req, app, err := ResolveMode(cfg, "aggressive", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeAggressive {
		t.Fatalf("expected requested aggressive, got %v", req)
	}
	if app != config.OptModeSafe {
		t.Fatalf("expected applied clamped to safe, got %v", app)
	}
}

func TestResolveMode_AggressiveAllowed(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = true
	req, app, err := ResolveMode(cfg, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeAggressive {
		t.Fatalf("expected requested aggressive, got %v", req)
	}
	if app != config.OptModeAggressive {
		t.Fatalf("expected applied aggressive, got %v", app)
	}
}

func TestResolveMode_AggressiveNotAllowedClamped(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = false
	req, app, err := ResolveMode(cfg, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeAggressive {
		t.Fatalf("expected requested aggressive, got %v", req)
	}
	if app != config.OptModeBalanced {
		t.Fatalf("expected applied clamped to balanced, got %v", app)
	}
}

func TestResolveMode_KeyMaxModeCapsApplied(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = true
	req, app, err := ResolveMode(cfg, "", "safe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeAggressive {
		t.Fatalf("expected requested aggressive, got %v", req)
	}
	if app != config.OptModeSafe {
		t.Fatalf("expected applied clamped to safe by keyMaxMode, got %v", app)
	}
}

func TestResolveMode_KeyMaxHigherThanServerMax(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	req, app, err := ResolveMode(cfg, "", "aggressive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeBalanced {
		t.Fatalf("expected requested balanced, got %v", req)
	}
	_ = app
	if app != config.OptModeBalanced {
		t.Fatalf("expected applied clamped to balanced (server max), got %v", app)
	}
}

func TestResolveMode_EmptyClientUsesServerDefault(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	req, app, err := ResolveMode(cfg, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeBalanced {
		t.Fatalf("expected requested balanced from server default, got %v", req)
	}
	if app != config.OptModeBalanced {
		t.Fatalf("expected applied balanced, got %v", app)
	}
}

func TestResolveMode_InvalidClientPreference(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	_, _, err := ResolveMode(cfg, "bogus", "")
	if err == nil {
		t.Fatal("expected error for invalid client preference")
	}
}

func TestResolveMode_InvalidKeyMaxMode(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	_, _, err := ResolveMode(cfg, "", "invalid-mode")
	if err == nil {
		t.Fatal("expected error for invalid keyMaxMode")
	}
}

func TestResolveMode_AllLayersCombined(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = true
	req, app, err := ResolveMode(cfg, "balanced", "safe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeBalanced {
		t.Fatalf("expected requested balanced, got %v", req)
	}
	if app != config.OptModeSafe {
		t.Fatalf("expected applied clamped to safe by keyMaxMode, got %v", app)
	}
}

func TestResolveMode_OffMode(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeOff)
	req, app, err := ResolveMode(cfg, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeOff {
		t.Fatalf("expected requested off, got %v", req)
	}
	if app != config.OptModeOff {
		t.Fatalf("expected applied off, got %v", app)
	}
}

// --- Engine tests ---

func TestEngine_DisabledReturnsUnchanged(t *testing.T) {
	cfg := testOptConfig(false, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	if eng.Enabled() {
		t.Fatal("expected engine to be disabled")
	}
	req := simpleRequest()
	origSystem := req.System
	origText := req.Messages[0].Content[0].Text
	out, res, rec, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.System != origSystem {
		t.Fatalf("system prompt modified by disabled engine")
	}
	if out.Messages[0].Content[0].Text != origText {
		t.Fatalf("message text modified by disabled engine")
	}
	if rec != nil {
		t.Fatalf("expected nil record for disabled engine")
	}
	if res.ModeApplied != config.OptModeSafe {
		t.Fatalf("expected default mode safe, got %v", res.ModeApplied)
	}
}

func TestEngine_OffModeBypassesTransformation(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeOff)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithANSI()
	origText := req.Messages[0].Content[0].Text
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Bypassed {
		t.Fatal("expected bypassed=true for off mode")
	}
	if req.Messages[0].Content[0].Text != origText {
		t.Fatalf("ANSI text should not be stripped in off mode")
	}
}

func TestEngine_SafeModeStripsANSI(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithANSI()
	origText := req.Messages[0].Content[0].Text
	t.Logf("original text: %q", origText)
	// Check mode resolution
	mi, err := eng.ResolveMode("", "")
	if err != nil {
		t.Fatalf("mode resolution error: %v", err)
	}
	t.Logf("applied mode: %v", mi.Applied)
	t.Logf("flags: %v", eng.modeFlags(mi.Applied))
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("applied mode from result: %v", res.ModeApplied)
	t.Logf("actions count: %d", len(res.Actions))
	for _, a := range res.Actions {
		t.Logf("  action: %s - %s", a.Kind, a.Description)
	}
	t.Logf("warnings count: %d", len(res.Warnings))
	for _, w := range res.Warnings {
		t.Logf("  warning: %s", w)
	}
	text := req.Messages[0].Content[0].Text
	t.Logf("final text: %q", text)
	if text != "red and normal" {
		t.Fatalf("expected ANSI stripped, got %q", text)
	}
}

func TestEngine_SafeModeStripsANSITokenSavings(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithANSI()
	before := resBeforeCount(req)
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RemovedTokensEstimated <= 0 {
		t.Fatalf("expected positive removed token estimate, got %d (before=%d)", res.RemovedTokensEstimated, before)
	}
}

func resBeforeCount(req *normalization.NormalizedRequest) int {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	e := newFallbackEstimator(cfg)
	total := 0
	total += e.countText(req.System)
	for _, m := range req.Messages {
		total += e.countText(messageText(m))
	}
	// Include tool definitions like CountRequest does.
	for _, t := range req.Tools {
		total += e.countText(t.Name) + e.countText(t.Description) + e.countText(marshalMap(t.InputSchema))
	}
	return total
}

func TestEngine_SafeModeCompactsJSON(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithJSONToolResult()
	origLen := len(req.Messages[0].Content[0].Text)
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	newLen := len(req.Messages[0].Content[0].Text)
	if newLen >= origLen {
		t.Fatalf("expected JSON compacted, orig=%d new=%d", origLen, newLen)
	}
	found := false
	for _, a := range res.Actions {
		if a.Kind == ActionJSONCompacted {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected JSON compacted action")
	}
}

func TestEngine_BalancedModeDeduplicates(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = true
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithDuplicateToolResults()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range res.Actions {
		if a.Kind == ActionDuplicateRemoved {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected duplicate removed action in balanced mode")
	}
}

func TestEngine_AggressiveModeCompactsLogs(t *testing.T) {
	cfg := testOptConfigAggressive()
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithLogs()
	origLen := len(req.Messages[0].Content[0].Text)
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	newLen := len(req.Messages[0].Content[0].Text)
	if newLen >= origLen {
		t.Fatalf("expected logs compacted, orig=%d new=%d", origLen, newLen)
	}
	found := false
	for _, a := range res.Actions {
		if a.Kind == ActionLogCompacted {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected log compacted action")
	}
}

func TestEngine_LossClassFromActions(t *testing.T) {
	cfg := testOptConfigAggressive()
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithLogs()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.LossClass != Selective {
		t.Fatalf("expected loss class selective, got %v", res.LossClass)
	}
}

func TestEngine_TokenCountsPopulated(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithTools()
	_, res, rec, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.InputTokensBefore <= 0 {
		t.Fatal("expected positive InputTokensBefore")
	}
	if res.InputTokensEstimated <= 0 {
		t.Fatal("expected positive InputTokensEstimated")
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.InputTokensBefore <= 0 {
		t.Fatal("expected positive record InputTokensBefore")
	}
}

func TestEngine_ProcessReturnsRecord(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := simpleRequest()
	_, _, rec, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.RequestID != "req-1" {
		t.Fatalf("expected request ID req-1, got %q", rec.RequestID)
	}
	if rec.ProviderID != "openai" {
		t.Fatalf("expected provider openai, got %q", rec.ProviderID)
	}
	if rec.ModelID != "gpt-4" {
		t.Fatalf("expected model gpt-4, got %q", rec.ModelID)
	}
}

func TestEngine_InvalidClientPreferenceReturnsError(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := simpleRequest()
	oc := defaultOC()
	oc.ClientPreference = "bogus"
	_, _, _, err := eng.Process(context.Background(), req, oc)
	if err == nil {
		t.Fatal("expected error for invalid client preference")
	}
}

func TestEngine_Compressors(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	if eng.Compressors() == nil {
		t.Fatal("expected non-nil compressors registry")
	}
}

func TestEngine_ModeInfo(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	mi, err := eng.ResolveMode("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mi.Applied != config.OptModeSafe {
		t.Fatalf("expected applied safe, got %v", mi.Applied)
	}
	if mi.Bypassed {
		t.Fatal("expected not bypassed for safe mode")
	}
}

func TestEngine_ModeInfoOff(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeOff)
	eng := NewEngine(cfg, nil, nullLogger())
	mi, err := eng.ResolveMode("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !mi.Bypassed {
		t.Fatal("expected bypassed for off mode")
	}
	if mi.Reason == "" {
		t.Fatal("expected non-empty bypass reason")
	}
}

// --- BuildProtected tests ---

func TestBuildProtected_SystemMessageImmutable(t *testing.T) {
	req := simpleRequest()
	p := BuildProtected(req)
	if !p.SystemProtected {
		t.Fatal("expected system to be protected")
	}
}

func TestBuildProtected_NoSystemMessage(t *testing.T) {
	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hi"}}},
		},
	}
	p := BuildProtected(req)
	if p.SystemProtected {
		t.Fatal("expected system not protected when empty")
	}
}

func TestBuildProtected_ToolSchemasProtected(t *testing.T) {
	req := requestWithTools()
	p := BuildProtected(req)
	if !p.ToolSchemasProtected {
		t.Fatal("expected tool schemas to be protected")
	}
	if len(p.ProtectedToolNames) != 2 {
		t.Fatalf("expected 2 protected tool names, got %d", len(p.ProtectedToolNames))
	}
	if !p.ProtectedToolNames["alpha"] {
		t.Fatal("expected alpha tool protected")
	}
	if !p.ProtectedToolNames["beta"] {
		t.Fatal("expected beta tool protected")
	}
}

func TestBuildProtected_UserMessageProtected(t *testing.T) {
	req := simpleRequest()
	p := BuildProtected(req)
	userMsg := req.Messages[0]
	if !p.IsProtectedMessage(userMsg) {
		t.Fatal("expected user message to be protected")
	}
}

func TestBuildProtected_AssistantNotProtected(t *testing.T) {
	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "response"}}},
		},
	}
	p := BuildProtected(req)
	assistantMsg := req.Messages[0]
	if p.IsProtectedMessage(assistantMsg) {
		t.Fatal("expected assistant message to not be protected")
	}
}

func TestBuildProtected_ToolResultLocations(t *testing.T) {
	req := requestWithToolResults()
	p := BuildProtected(req)
	if len(p.ProtectedSubstrings) == 0 {
		t.Fatal("expected protected substrings from tool results with file:line")
	}
	found := false
	for _, s := range p.ProtectedSubstrings {
		if s == "src/main.go:42" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected src/main.go:42 in protected substrings")
	}
}

func TestProtectedContent_Contains(t *testing.T) {
	req := requestWithToolResults()
	p := BuildProtected(req)
	if !p.Contains("see error at src/main.go:42 for details") {
		t.Fatal("expected Contains to find protected substring")
	}
	if p.Contains("no match here at all") {
		t.Fatal("expected Contains to not find non-existent substring")
	}
}

func TestBuildProtected_SystemRoleProtected(t *testing.T) {
	req := simpleRequest()
	p := BuildProtected(req)
	sysMsg := normalization.Message{Role: normalization.RoleSystem, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "system"}}}
	if !p.IsProtectedMessage(sysMsg) {
		t.Fatal("expected system role message to be protected")
	}
}

func TestBuildProtected_ToolRoleNotProtected(t *testing.T) {
	req := requestWithToolResults()
	p := BuildProtected(req)
	toolMsg := normalization.Message{Role: normalization.RoleTool, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "result"}}}
	if p.IsProtectedMessage(toolMsg) {
		t.Fatal("expected tool role message to not be protected")
	}
}

// --- Estimator tests ---

func TestEstimator_CountTextReturnsReasonableEstimate(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	e := newFallbackEstimator(cfg)
	n, err := e.CountText("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n <= 0 {
		t.Fatal("expected positive token count for non-empty text")
	}
	if n > 100 {
		t.Fatalf("token count %d seems unreasonably high for 11 chars", n)
	}
}

func TestEstimator_CountTextEmpty(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	e := newFallbackEstimator(cfg)
	n, err := e.CountText("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 tokens for empty text, got %d", n)
	}
}

func TestEstimator_CountTextDefaultCharsPerToken(t *testing.T) {
	cfg := config.TokenEstimationConfig{}
	e := newFallbackEstimator(cfg)
	n, err := e.CountText("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n <= 0 {
		t.Fatal("expected positive token count")
	}
}

func TestEstimator_EstimateReturnsValidBreakdown(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := requestWithTools()
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.Total <= 0 {
		t.Fatal("expected positive total tokens")
	}
	if bd.System <= 0 {
		t.Fatal("expected positive system tokens")
	}
	if bd.ToolDefinitions <= 0 {
		t.Fatal("expected positive tool definition tokens")
	}
	if bd.Source != "estimated" {
		t.Fatalf("expected source estimated, got %q", bd.Source)
	}
}

func TestEstimator_EstimateNoTools(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := simpleRequest()
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.Total <= 0 {
		t.Fatal("expected positive total tokens")
	}
	if bd.ToolDefinitions != 0 {
		t.Fatalf("expected 0 tool definition tokens, got %d", bd.ToolDefinitions)
	}
}

func TestEstimator_CountTextWithRegistry(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	n := est.CountText("hello", "openai", "gpt-4")
	if n <= 0 {
		t.Fatal("expected positive count from registry")
	}
}

func TestEstimator_Name(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	e := newFallbackEstimator(cfg)
	if e.Name() != "fallback-chars" {
		t.Fatalf("expected name fallback-chars, got %q", e.Name())
	}
}

func TestEstimator_SupportsAll(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	e := newFallbackEstimator(cfg)
	if !e.Supports("any-provider", "any-model") {
		t.Fatal("fallback estimator should support all providers/models")
	}
}

func TestEstimator_ToolResultTokens(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := requestWithToolResults()
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.ToolResults <= 0 {
		t.Fatal("expected positive tool result tokens")
	}
}

// --- RecordFromResult tests ---

func TestRecordFromResult(t *testing.T) {
	oc := defaultOC()
	oc.RouteName = "test-route"
	oc.ClientKeyID = "key-123"
	res := &OptimizationResult{
		ModeRequested:           config.OptModeSafe,
		ModeApplied:             config.OptModeSafe,
		LossClass:               Lossless,
		Reversible:              true,
		InputTokensBefore:       100,
		InputTokensEstimated:    90,
		ExpectedCachedTokens:    20,
		CompressionTokens:       50,
		EstimatedGrossSavingUSD: 0.01,
		EstimatedOptimizerCost:  0.001,
		EstimatedNetSavingUSD:   0.009,
		Bypassed:                false,
		LUIVersion:              "1.0",
		LUIRenderer:             "native_prompt",
		Actions: []Action{
			{Kind: ActionANSIStripped, Description: "stripped ANSI", LossClass: Lossless},
			{Kind: ActionJSONCompacted, Description: "compacted JSON", LossClass: Lossless},
		},
	}
	rec := RecordFromResult(oc, res)
	if rec.RequestID != "req-1" {
		t.Fatalf("expected request ID req-1, got %q", rec.RequestID)
	}
	if rec.RouteName != "test-route" {
		t.Fatalf("expected route name test-route, got %q", rec.RouteName)
	}
	if rec.ClientKeyID != "key-123" {
		t.Fatalf("expected client key ID key-123, got %q", rec.ClientKeyID)
	}
	if rec.ModeRequested != "safe" {
		t.Fatalf("expected mode requested safe, got %q", rec.ModeRequested)
	}
	if rec.ModeApplied != "safe" {
		t.Fatalf("expected mode applied safe, got %q", rec.ModeApplied)
	}
	if rec.InputTokensBefore != 100 {
		t.Fatalf("expected input tokens before 100, got %d", rec.InputTokensBefore)
	}
	if rec.InputTokensAfterEstimated != 90 {
		t.Fatalf("expected input tokens after 90, got %d", rec.InputTokensAfterEstimated)
	}
	if rec.LossClass != "lossless" {
		t.Fatalf("expected loss class lossless, got %q", rec.LossClass)
	}
	if rec.LUIVersion != "1.0" {
		t.Fatalf("expected LUI version 1.0, got %q", rec.LUIVersion)
	}
	if rec.Renderer != "native_prompt" {
		t.Fatalf("expected renderer native_prompt, got %q", rec.Renderer)
	}
	if rec.Status != RecordPending {
		t.Fatalf("expected status pending for newly created record, got %q", rec.Status)
	}
}

func TestRecordFromResult_ActionsJSON(t *testing.T) {
	oc := defaultOC()
	res := &OptimizationResult{
		ModeApplied: config.OptModeSafe,
		LossClass:   Lossless,
		Actions: []Action{
			{Kind: ActionANSIStripped},
			{Kind: ActionJSONCompacted},
		},
	}
	rec := RecordFromResult(oc, res)
	if rec.OptimizersJSON == "" {
		t.Fatal("expected non-empty optimizers JSON")
	}
	if rec.EstimatorsJSON == "" {
		t.Fatal("expected non-empty estimators JSON")
	}
}

func TestRecordFromResult_Bypassed(t *testing.T) {
	oc := defaultOC()
	res := &OptimizationResult{
		ModeApplied:  config.OptModeOff,
		LossClass:    Lossless,
		Bypassed:     true,
		BypassReason: "optimization disabled by policy",
	}
	rec := RecordFromResult(oc, res)
	if !rec.Bypassed {
		t.Fatal("expected record bypassed=true")
	}
	if rec.BypassReason != "optimization disabled by policy" {
		t.Fatalf("expected bypass reason, got %q", rec.BypassReason)
	}
	if rec.Status != RecordPending {
		t.Fatalf("expected status pending for newly created record, got %q", rec.Status)
	}
}

func TestRecordFromResult_ZeroValues(t *testing.T) {
	oc := defaultOC()
	res := &OptimizationResult{
		ModeApplied: config.OptModeSafe,
		LossClass:   Lossless,
	}
	rec := RecordFromResult(oc, res)
	if rec.GrossSavingUSD != 0 {
		t.Fatalf("expected 0 gross saving, got %f", rec.GrossSavingUSD)
	}
	if rec.NetSavingUSD != 0 {
		t.Fatalf("expected 0 net saving, got %f", rec.NetSavingUSD)
	}
	if rec.Status != RecordPending {
		t.Fatalf("expected status pending for newly created record, got %q", rec.Status)
	}
}

func TestRecord_FinalizeWithActuals(t *testing.T) {
	rec := Record{}
	rec.FinalizeWithActuals(100, 200, 300, 50)
	if rec.ProviderInputTokensActual != 100 {
		t.Fatalf("expected 100 input actuals, got %d", rec.ProviderInputTokensActual)
	}
	if rec.ProviderOutputTokensActual != 200 {
		t.Fatalf("expected 200 output actuals, got %d", rec.ProviderOutputTokensActual)
	}
	if rec.CacheReadTokensActual != 300 {
		t.Fatalf("expected 300 cache read tokens, got %d", rec.CacheReadTokensActual)
	}
	if rec.CacheWriteTokensActual != 50 {
		t.Fatalf("expected 50 cache write tokens, got %d", rec.CacheWriteTokensActual)
	}
	if rec.CacheStatus != "cache_reported_by_provider" {
		t.Fatalf("expected cache status cache_reported_by_provider, got %q", rec.CacheStatus)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("expected default config to be disabled")
	}
	if cfg.DefaultMode != config.OptModeSafe {
		t.Fatalf("expected default mode safe, got %v", cfg.DefaultMode)
	}
}

// --- Utility function tests ---

func TestExtractLocations(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{name: "single location", text: "error at src/main.go:42", expected: 1},
		{name: "multiple locations", text: "see src/main.go:42 and pkg/util.go:100", expected: 2},
		{name: "no location", text: "just plain text", expected: 0},
		{name: "empty", text: "", expected: 0},
		{name: "deep path", text: "at /home/user/project/src/file.go:1234", expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := extractLocations(tc.text)
			if len(result) != tc.expected {
				t.Fatalf("expected %d locations, got %d: %v", tc.expected, len(result), result)
			}
		})
	}
}

func TestExtractLocations_PreservesExactMatch(t *testing.T) {
	result := extractLocations("error at src/main.go:42")
	if len(result) != 1 {
		t.Fatalf("expected 1 location, got %d", len(result))
	}
	if result[0] != "src/main.go:42" {
		t.Fatalf("expected src/main.go:42, got %q", result[0])
	}
}

func TestCompactJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "spaces removed", input: `{"key": "value"}`, expected: `{"key":"value"}`},
		{name: "tabs removed", input: "{\"key\":\t\"value\"}", expected: `{"key":"value"}`},
		{name: "newlines removed", input: "{\n\"key\": \"value\"\n}", expected: `{"key":"value"}`},
		{name: "nested", input: `{"a": {"b": 1}}`, expected: `{"a":{"b":1}}`},
		{name: "strings preserved", input: `{"key": "value with spaces"}`, expected: `{"key":"value with spaces"}`},
		{name: "escaped quotes", input: `{"key": "say \"hello\""}`, expected: `{"key":"say \"hello\""}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := compactJSON(tc.input)
			if !ok {
				t.Fatalf("compactJSON returned false")
			}
			if result != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestHashText(t *testing.T) {
	h1 := hashText("hello")
	h2 := hashText("hello")
	h3 := hashText("world")
	if h1 != h2 {
		t.Fatalf("expected same hash for same input, got %q and %q", h1, h2)
	}
	if h1 == h3 {
		t.Fatalf("expected different hash for different input")
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{input: 0, expected: "0"},
		{input: 1, expected: "1"},
		{input: 10, expected: "10"},
		{input: 123, expected: "123"},
		{input: 999, expected: "999"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			result := itoa(tc.input)
			if result != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestOcHasFlag(t *testing.T) {
	oc := OptimizationContext{Flags: []string{"strip_ansi", "compact_json"}}
	if !ocHasFlag(oc, "strip_ansi") {
		t.Fatal("expected ocHasFlag to find strip_ansi")
	}
	if !ocHasFlag(oc, "compact_json") {
		t.Fatal("expected ocHasFlag to find compact_json")
	}
	if ocHasFlag(oc, "deduplicate") {
		t.Fatal("expected ocHasFlag to not find deduplicate")
	}
}

func TestOcHasFlagEmpty(t *testing.T) {
	oc := OptimizationContext{}
	if ocHasFlag(oc, "strip_ansi") {
		t.Fatal("expected ocHasFlag to not find anything in empty flags")
	}
}

// --- Mutually exclusive token region tests (Section 4) ---

func TestEstimator_MutuallyExclusiveRegions_OneUserMessage(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := simpleRequest() // one user message, no tools, no tool results
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.CurrentUser <= 0 {
		t.Fatal("expected positive CurrentUser tokens")
	}
	if bd.MessageHistory != 0 {
		t.Fatalf("expected 0 MessageHistory for single user message, got %d", bd.MessageHistory)
	}
	if bd.ToolResults != 0 {
		t.Fatalf("expected 0 ToolResults, got %d", bd.ToolResults)
	}
	sum := bd.System + bd.MessageHistory + bd.CurrentUser + bd.ToolDefinitions + bd.ToolResults + bd.ProtocolOverhead
	if sum != bd.Total {
		t.Fatalf("sum of regions %d != Total %d", sum, bd.Total)
	}
}

func TestEstimator_MutuallyExclusiveRegions_OlderUserInHistory(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := &normalization.NormalizedRequest{
		System: "sys",
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "old user"}}},
			{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "assistant reply"}}},
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "current user"}}},
		},
	}
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.CurrentUser <= 0 {
		t.Fatal("expected positive CurrentUser for last user message")
	}
	if bd.MessageHistory <= 0 {
		t.Fatal("expected positive MessageHistory for older messages")
	}
	sum := bd.System + bd.MessageHistory + bd.CurrentUser + bd.ToolDefinitions + bd.ToolResults + bd.ProtocolOverhead
	if sum != bd.Total {
		t.Fatalf("sum of regions %d != Total %d", sum, bd.Total)
	}
}

func TestEstimator_MutuallyExclusiveRegions_ToolResultsOnly(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := requestWithToolResults()
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.ToolResults <= 0 {
		t.Fatal("expected positive ToolResults")
	}
	// User message should be in CurrentUser (last user), tool results in ToolResults.
	if bd.CurrentUser <= 0 {
		t.Fatal("expected positive CurrentUser")
	}
	sum := bd.System + bd.MessageHistory + bd.CurrentUser + bd.ToolDefinitions + bd.ToolResults + bd.ProtocolOverhead
	if sum != bd.Total {
		t.Fatalf("sum of regions %d != Total %d", sum, bd.Total)
	}
}

func TestEstimator_MutuallyExclusiveRegions_SystemOnly(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := &normalization.NormalizedRequest{System: "You are helpful."}
	bd := est.Estimate(req, "openai", "gpt-4")
	if bd.System <= 0 {
		t.Fatal("expected positive System tokens")
	}
	if bd.CurrentUser != 0 {
		t.Fatalf("expected 0 CurrentUser, got %d", bd.CurrentUser)
	}
	if bd.MessageHistory != 0 {
		t.Fatalf("expected 0 MessageHistory, got %d", bd.MessageHistory)
	}
	sum := bd.System + bd.MessageHistory + bd.CurrentUser + bd.ToolDefinitions + bd.ToolResults + bd.ProtocolOverhead
	if sum != bd.Total {
		t.Fatalf("sum of regions %d != Total %d", sum, bd.Total)
	}
}

func TestEstimator_EmptyRequestZeroContentTokens(t *testing.T) {
	cfg := config.TokenEstimationConfig{FallbackCharsPerToken: 3.5, SafetyMultiplier: 1.15}
	est := newEstimatorRegistry(cfg, nil)
	req := &normalization.NormalizedRequest{}
	bd := est.Estimate(req, "openai", "gpt-4")
	// Empty request has zero content in all semantic regions.
	if bd.System != 0 {
		t.Fatalf("expected 0 System, got %d", bd.System)
	}
	if bd.CurrentUser != 0 {
		t.Fatalf("expected 0 CurrentUser, got %d", bd.CurrentUser)
	}
	if bd.MessageHistory != 0 {
		t.Fatalf("expected 0 MessageHistory, got %d", bd.MessageHistory)
	}
	if bd.ToolResults != 0 {
		t.Fatalf("expected 0 ToolResults, got %d", bd.ToolResults)
	}
	if bd.ToolDefinitions != 0 {
		t.Fatalf("expected 0 ToolDefinitions, got %d", bd.ToolDefinitions)
	}
	// ProtocolOverhead may be non-zero (JSON structural tokens) but must not be negative.
	if bd.ProtocolOverhead < 0 {
		t.Fatalf("expected non-negative protocol overhead, got %d", bd.ProtocolOverhead)
	}
}

func TestEstimator_SavingsDerivedFromCorrectedTotals(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithANSI()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Savings should be non-negative and derived from corrected (non-inflated) totals.
	if res.RemovedTokensEstimated < 0 {
		t.Fatalf("expected non-negative savings, got %d", res.RemovedTokensEstimated)
	}
	if res.InputTokensBefore <= 0 {
		t.Fatal("expected positive InputTokensBefore")
	}
	if res.InputTokensEstimated <= 0 {
		t.Fatal("expected positive InputTokensEstimated")
	}
}

// --- Transactional optimizer execution tests (Section 5) ---

func TestCloneRequest_DeepCopy(t *testing.T) {
	req := simpleRequest()
	clone := CloneRequest(req)
	if clone == nil {
		t.Fatal("expected non-nil clone")
	}
	// Mutate clone; original must be untouched.
	clone.Messages[0].Content[0].Text = "mutated"
	if req.Messages[0].Content[0].Text == "mutated" {
		t.Fatal("clone mutation leaked into original")
	}
}

func TestCloneRequest_Nil(t *testing.T) {
	if CloneRequest(nil) != nil {
		t.Fatal("CloneRequest(nil) should return nil")
	}
}

func TestProtectedFingerprint_Consistent(t *testing.T) {
	req := simpleRequest()
	fp1 := ProtectedFingerprint(req)
	fp2 := ProtectedFingerprint(req)
	if fp1 != fp2 {
		t.Fatalf("expected same fingerprint, got %q and %q", fp1, fp2)
	}
	if fp1 == "" {
		t.Fatal("expected non-empty fingerprint")
	}
}

func TestProtectedFingerprint_DetectsSystemMutation(t *testing.T) {
	req := simpleRequest()
	fp1 := ProtectedFingerprint(req)
	clone := CloneRequest(req)
	clone.System = "mutated"
	fp2 := ProtectedFingerprint(clone)
	if fp1 == fp2 {
		t.Fatal("expected different fingerprint after system mutation")
	}
}

func TestProtectedFingerprint_DetectsToolNameMutation(t *testing.T) {
	req := requestWithTools()
	fp1 := ProtectedFingerprint(req)
	clone := CloneRequest(req)
	clone.Tools[0].Name = "mutated"
	fp2 := ProtectedFingerprint(clone)
	if fp1 == fp2 {
		t.Fatal("expected different fingerprint after tool name mutation")
	}
}

func TestProtectedFingerprint_UserMessageMutationDetected(t *testing.T) {
	req := simpleRequest()
	fp1 := ProtectedFingerprint(req)
	clone := CloneRequest(req)
	clone.Messages[0].Content[0].Text = "mutated"
	fp2 := ProtectedFingerprint(clone)
	// Per Item #26 user request text is included in the fingerprint; only
	// transformations explicitly proven safe and allowed may modify it.
	if fp1 == fp2 {
		t.Fatal("expected different fingerprint after user message mutation")
	}
}

// failingOptimizer always returns an error after the caller has had a chance
// to observe that it was called.
type failingOptimizer struct {
	called *bool
}

func (f failingOptimizer) Name() string    { return "failing_optimizer" }
func (f failingOptimizer) Version() string { return "1.0" }
func (f failingOptimizer) Supports(oc OptimizationContext, mode config.OptimizationMode) bool {
	return true
}
func (f failingOptimizer) Optimize(ctx context.Context, req *normalization.NormalizedRequest, oc OptimizationContext, mode config.OptimizationMode, res *OptimizationResult) error {
	*f.called = true
	// Mutate the request to prove rollback works.
	for i := range req.Messages {
		for j := range req.Messages[i].Content {
			req.Messages[i].Content[j].Text = "MUTATED BY FAILING OPTIMIZER"
		}
	}
	return fmt.Errorf("optimizer intentionally failed")
}

func TestTransactionalOptimizerFailureLeavesRequestUnchanged(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := simpleRequest()
	origText := req.Messages[0].Content[0].Text

	called := false
	// Inject a failing optimizer at the front of the pipeline.
	eng.optimizers = append([]Optimizer{failingOptimizer{called: &called}}, eng.optimizers...)

	_, _, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected failing optimizer to be called")
	}
	// The original request must be unchanged.
	if req.Messages[0].Content[0].Text != origText {
		t.Fatalf("request was mutated despite optimizer failure: got %q, want %q",
			req.Messages[0].Content[0].Text, origText)
	}
}

func TestTransactionalOptimizerSuccessCommitsChanges(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithANSI()
	// Run Process — the ANSI optimizer should strip escapes and commit.
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range res.Actions {
		if a.Kind == ActionANSIStripped {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected ANSI stripped action")
	}
	text := req.Messages[0].Content[0].Text
	if text != "red and normal" {
		t.Fatalf("expected ANSI stripped, got %q", text)
	}
}

func TestTransactionalSequentialStagesSeeCommittedOutput(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	// Request with both ANSI escapes and JSON tool result — both stages should apply.
	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{
				Role:    normalization.RoleTool,
				Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "\x1b[32mgreen\x1b[0m"}},
			},
			{
				Role:    normalization.RoleTool,
				Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: `{"a": 1,  "b": 2}`}},
			},
		},
	}
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both ANSI and JSON actions should be present.
	hasANSI, hasJSON := false, false
	for _, a := range res.Actions {
		if a.Kind == ActionANSIStripped {
			hasANSI = true
		}
		if a.Kind == ActionJSONCompacted {
			hasJSON = true
		}
	}
	if !hasANSI {
		t.Fatal("expected ANSI stripped action")
	}
	if !hasJSON {
		t.Fatal("expected JSON compacted action")
	}
}

// --- Transactional optimizer execution tests (Section 5) ---

func TestModeName(t *testing.T) {
	if ModeName(config.OptModeSafe) != "safe" {
		t.Fatalf("expected safe, got %q", ModeName(config.OptModeSafe))
	}
	if ModeName(config.OptModeOff) != "off" {
		t.Fatalf("expected off, got %q", ModeName(config.OptModeOff))
	}
	if ModeName(config.OptModeBalanced) != "balanced" {
		t.Fatalf("expected balanced, got %q", ModeName(config.OptModeBalanced))
	}
	if ModeName(config.OptModeAggressive) != "aggressive" {
		t.Fatalf("expected aggressive, got %q", ModeName(config.OptModeAggressive))
	}
}

// --- BuildProtected edge cases ---

func TestBuildProtected_EmptyRequest(t *testing.T) {
	req := &normalization.NormalizedRequest{}
	p := BuildProtected(req)
	if p.SystemProtected {
		t.Fatal("expected system not protected for empty request")
	}
	if !p.CurrentUserProtected {
		t.Fatal("expected CurrentUserProtected to be true by default")
	}
	if !p.ToolSchemasProtected {
		t.Fatal("expected ToolSchemasProtected to be true by default")
	}
}

func TestBuildProtected_MultipleTools(t *testing.T) {
	req := &normalization.NormalizedRequest{
		Tools: []normalization.Tool{
			{Name: "search"},
			{Name: "calculate"},
			{Name: "write"},
		},
	}
	p := BuildProtected(req)
	if len(p.ProtectedToolNames) != 3 {
		t.Fatalf("expected 3 protected tool names, got %d", len(p.ProtectedToolNames))
	}
	for _, name := range []string{"search", "calculate", "write"} {
		if !p.ProtectedToolNames[name] {
			t.Fatalf("expected tool %q to be protected", name)
		}
	}
}

func TestBuildProtected_NoToolResults(t *testing.T) {
	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hi"}}},
		},
	}
	p := BuildProtected(req)
	if len(p.ProtectedSubstrings) != 0 {
		t.Fatalf("expected no protected substrings, got %d", len(p.ProtectedSubstrings))
	}
}

// --- Output budget tests ---

func TestOutputBudgetOptimizer_Supports(t *testing.T) {
	o := outputBudgetOptimizer{}
	oc := OptimizationContext{OutputBudgetMode: "adaptive"}
	if !o.Supports(oc, config.OptModeSafe) {
		t.Fatal("expected output budget to support safe mode")
	}
	if o.Supports(oc, config.OptModeOff) {
		t.Fatal("expected output budget to not support off mode")
	}
	oc2 := OptimizationContext{OutputBudgetMode: "off"}
	if o.Supports(oc2, config.OptModeSafe) {
		t.Fatal("expected output budget to not support when mode is off")
	}
}

func TestOutputBudgetOptimizer_ExplicitClientMax(t *testing.T) {
	maxTokens := 512
	o := outputBudgetOptimizer{cfg: config.OutputBudgetConfig{Mode: "adaptive"}}
	req := simpleRequest()
	req.MaxOutputTokens = &maxTokens
	oc := OptimizationContext{OutputBudgetMode: "adaptive"}
	res := &OptimizationResult{}
	err := o.Optimize(context.Background(), req, oc, config.OptModeSafe, res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *req.MaxOutputTokens != 512 {
		t.Fatalf("expected explicit max preserved at 512, got %d", *req.MaxOutputTokens)
	}
	found := false
	for _, a := range res.Actions {
		if a.Kind == ActionOutputBudget {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected output budget action recorded")
	}
}

func TestOutputBudgetOptimizer_AdaptiveSimple(t *testing.T) {
	o := outputBudgetOptimizer{cfg: config.OutputBudgetConfig{
		Mode:             "adaptive",
		SimpleMaxTokens:  256,
		MediumMaxTokens:  1024,
		ComplexMaxTokens: 2048,
		DefaultMaxTokens: 512,
	}}
	req := simpleRequest()
	oc := OptimizationContext{
		OutputBudgetMode: "adaptive",
		Complexity:       "simple",
	}
	res := &OptimizationResult{}
	err := o.Optimize(context.Background(), req, oc, config.OptModeBalanced, res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.MaxOutputTokens == nil {
		t.Fatal("expected MaxOutputTokens to be set")
	}
	if *req.MaxOutputTokens != 256 {
		t.Fatalf("expected 256 for simple, got %d", *req.MaxOutputTokens)
	}
}

func TestOutputBudgetOptimizer_AdaptiveComplex(t *testing.T) {
	o := outputBudgetOptimizer{cfg: config.OutputBudgetConfig{
		Mode:             "adaptive",
		SimpleMaxTokens:  256,
		MediumMaxTokens:  1024,
		ComplexMaxTokens: 4096,
		DefaultMaxTokens: 512,
	}}
	req := simpleRequest()
	oc := OptimizationContext{
		OutputBudgetMode: "adaptive",
		Complexity:       "complex",
	}
	res := &OptimizationResult{}
	err := o.Optimize(context.Background(), req, oc, config.OptModeBalanced, res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.MaxOutputTokens == nil {
		t.Fatal("expected MaxOutputTokens to be set")
	}
	if *req.MaxOutputTokens != 4096 {
		t.Fatalf("expected 4096 for complex, got %d", *req.MaxOutputTokens)
	}
}

func TestOutputBudgetOptimizer_ModeOff(t *testing.T) {
	o := outputBudgetOptimizer{cfg: config.OutputBudgetConfig{Mode: "off"}}
	req := simpleRequest()
	oc := OptimizationContext{OutputBudgetMode: "adaptive"}
	res := &OptimizationResult{}
	err := o.Optimize(context.Background(), req, oc, config.OptModeBalanced, res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.MaxOutputTokens != nil {
		t.Fatalf("expected MaxOutputTokens to remain nil when mode off, got %v", req.MaxOutputTokens)
	}
}

func TestOutputBudgetOptimizer_DefaultTokens(t *testing.T) {
	o := outputBudgetOptimizer{cfg: config.OutputBudgetConfig{Mode: "adaptive"}}
	req := simpleRequest()
	oc := OptimizationContext{
		OutputBudgetMode: "adaptive",
		Complexity:       "",
	}
	res := &OptimizationResult{}
	err := o.Optimize(context.Background(), req, oc, config.OptModeBalanced, res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.MaxOutputTokens == nil {
		t.Fatal("expected MaxOutputTokens to be set")
	}
	if *req.MaxOutputTokens != 2048 {
		t.Fatalf("expected 2048 default, got %d", *req.MaxOutputTokens)
	}
}

// --- Optimizer Supports edge cases ---

func TestJsonCompactOptimizer_Supports(t *testing.T) {
	o := jsonCompactOptimizer{}
	oc := OptimizationContext{Flags: []string{"compact_json"}}
	if !o.Supports(oc, config.OptModeSafe) {
		t.Fatal("expected json compact to support safe mode with flag")
	}
	ocNoFlag := OptimizationContext{Flags: []string{}}
	if o.Supports(ocNoFlag, config.OptModeSafe) {
		t.Fatal("expected json compact to not support without flag")
	}
	if o.Supports(oc, config.OptModeOff) {
		t.Fatal("expected json compact to not support off mode")
	}
}

func TestDedupeOptimizer_Supports(t *testing.T) {
	o := dedupeOptimizer{}
	oc := OptimizationContext{Flags: []string{"deduplicate"}}
	if o.Supports(oc, config.OptModeSafe) {
		t.Fatal("expected dedupe to NOT support safe mode")
	}
	if !o.Supports(oc, config.OptModeBalanced) {
		t.Fatal("expected dedupe to support balanced mode with flag")
	}
	if !o.Supports(oc, config.OptModeAggressive) {
		t.Fatal("expected dedupe to support aggressive mode with flag")
	}
	ocNoFlag := OptimizationContext{}
	if o.Supports(ocNoFlag, config.OptModeSafe) {
		t.Fatal("expected dedupe to not support without flag")
	}
	if o.Supports(oc, config.OptModeOff) {
		t.Fatal("expected dedupe to not support off mode")
	}
}

func TestLogCompactOptimizer_Supports(t *testing.T) {
	o := logCompactOptimizer{}
	oc := OptimizationContext{Flags: []string{"compact_logs"}}
	if !o.Supports(oc, config.OptModeBalanced) {
		t.Fatal("expected log compact to support balanced mode with flag")
	}
	if !o.Supports(oc, config.OptModeAggressive) {
		t.Fatal("expected log compact to support aggressive mode with flag")
	}
	if o.Supports(oc, config.OptModeSafe) {
		t.Fatal("expected log compact to not support safe mode")
	}
	if o.Supports(oc, config.OptModeOff) {
		t.Fatal("expected log compact to not support off mode")
	}
}

func TestLogCompactOptimizer_NoFlagBypassesMode(t *testing.T) {
	o := logCompactOptimizer{}
	// Aggressive mode alone without the flag should NOT activate.
	ocNoFlag := OptimizationContext{Flags: []string{}}
	if o.Supports(ocNoFlag, config.OptModeAggressive) {
		t.Fatal("expected log compact to not support aggressive mode without flag")
	}
	if o.Supports(ocNoFlag, config.OptModeBalanced) {
		t.Fatal("expected log compact to not support balanced mode without flag")
	}
}

func TestAnsiOptimizer_RequiresFlag(t *testing.T) {
	o := ansiOptimizer{}
	// Mode safe without strip_ansi flag should not activate.
	ocNoFlag := OptimizationContext{Flags: []string{}}
	if o.Supports(ocNoFlag, config.OptModeSafe) {
		t.Fatal("expected ANSI to not support safe mode without strip_ansi flag")
	}
	// Mode off should never activate.
	ocWithFlag := OptimizationContext{Flags: []string{"strip_ansi"}}
	if o.Supports(ocWithFlag, config.OptModeOff) {
		t.Fatal("expected ANSI to not support off mode")
	}
}

func TestDedupeOptimizer_ClassificationIsSelective(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = true
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithDuplicateToolResults()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range res.Actions {
		if a.Kind == ActionDuplicateRemoved {
			if a.LossClass != Selective {
				t.Fatalf("expected dedup classification selective, got %v", a.LossClass)
			}
			if a.Reversible {
				t.Fatal("expected dedup to be non-reversible (selective)")
			}
			return
		}
	}
	t.Fatal("expected duplicate removed action")
}

func TestConversationOptimizer_Supports(t *testing.T) {
	o := conversationOptimizer{}
	oc := OptimizationContext{ConversationEnabled: true}
	if !o.Supports(oc, config.OptModeBalanced) {
		t.Fatal("expected conversation to support balanced when enabled")
	}
	if o.Supports(oc, config.OptModeOff) {
		t.Fatal("expected conversation to not support off mode")
	}
	ocDisabled := OptimizationContext{ConversationEnabled: false}
	if o.Supports(ocDisabled, config.OptModeBalanced) {
		t.Fatal("expected conversation to not support when disabled")
	}
}

// --- CompactLog edge cases ---

func TestCompactLog_Empty(t *testing.T) {
	out, changed, saved := compactLog("", nil)
	if out != "" {
		t.Fatalf("expected empty output, got %q", out)
	}
	if changed {
		t.Fatal("expected no change for empty text")
	}
	if saved != 0 {
		t.Fatalf("expected 0 saved, got %d", saved)
	}
}

func TestCompactLog_NoRepeatedLines(t *testing.T) {
	text := "line1\nline2\nline3"
	out, changed, _ := compactLog(text, nil)
	if changed {
		t.Fatal("expected no change for unique lines")
	}
	if out != text {
		t.Fatalf("expected unchanged output, got %q", out)
	}
}

func TestCompactLog_RepeatedLines(t *testing.T) {
	text := "line1\nline1\nline1\nline2"
	out, changed, saved := compactLog(text, nil)
	if !changed {
		t.Fatal("expected change for repeated lines")
	}
	if saved <= 0 {
		t.Fatalf("expected positive saved bytes, got %d", saved)
	}
	if out == text {
		t.Fatalf("expected output to differ from input")
	}
}

func TestCompactLog_PreservesErrors(t *testing.T) {
	text := "INFO ok\nERROR something broke\nINFO ok\nINFO ok"
	out, changed, _ := compactLog(text, nil)
	if !changed {
		t.Fatal("expected change")
	}
	if !containsSubstring(out, "ERROR something broke") {
		t.Fatalf("expected error line preserved, got %q", out)
	}
}

func TestCompactLog_PreservesBlankLines(t *testing.T) {
	text := "line1\nline1\n\nline2\nline2"
	out, changed, _ := compactLog(text, nil)
	if !changed {
		t.Fatal("expected change")
	}
	if !containsSubstring(out, "\n\n") {
		t.Fatalf("expected blank line preserved, got %q", out)
	}
	if !containsSubstring(out, "(x2)") {
		t.Fatalf("expected repetition collapsed, got %q", out)
	}
}

func TestCompactLog_PreservesProtectedSubstrings(t *testing.T) {
	prot := &ProtectedContent{
		ProtectedSubstrings: []string{"src/main.go:42"},
	}
	text := "result at src/main.go:42\nsome line\nsome line"
	out, changed, _ := compactLog(text, prot)
	if !changed {
		t.Fatal("expected change")
	}
	if !containsSubstring(out, "src/main.go:42") {
		t.Fatalf("expected protected substring preserved, got %q", out)
	}
	if !containsSubstring(out, "(x2)") {
		t.Fatalf("expected repetition collapsed, got %q", out)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsRunes(s, sub))
}

func containsRunes(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- FinalizeAndPersist tests ---

func TestFinalizeAndPersist_NilRecord(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	eng.FinalizeAndPersist(context.Background(), nil, 100, 200, 300)
	// Should not panic
}

func TestFinalizeAndPersist_NilStore(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-1"}
	eng.FinalizeAndPersist(context.Background(), rec, 100, 200, 300)
	if rec.ProviderInputTokensActual != 100 {
		t.Fatalf("expected 100 input actuals, got %d", rec.ProviderInputTokensActual)
	}
	if rec.ProviderOutputTokensActual != 200 {
		t.Fatalf("expected 200 output actuals, got %d", rec.ProviderOutputTokensActual)
	}
	if rec.CacheReadTokensActual != 300 {
		t.Fatalf("expected 300 cache read actuals, got %d", rec.CacheReadTokensActual)
	}
}

func TestFinalizeAndPersist_NegativeActualsIgnored(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-1", ProviderInputTokensActual: 50}
	eng.FinalizeAndPersist(context.Background(), rec, -1, -1, -1)
	if rec.ProviderInputTokensActual != 50 {
		t.Fatalf("expected original value preserved, got %d", rec.ProviderInputTokensActual)
	}
}

// --- Optimizer Name/Version ---

func TestOptimizerNamesAndVersions(t *testing.T) {
	tests := []struct {
		opt     Optimizer
		name    string
		version string
	}{
		{ansiOptimizer{}, "ansi_strip", "1.0"},
		{jsonCompactOptimizer{}, "json_compact", "1.0"},
		{dedupeOptimizer{}, "dedupe", "1.0"},
		{logCompactOptimizer{}, "log_compact", "1.0"},
		{conversationOptimizer{}, "conversation_window", "1.0"},
		{outputBudgetOptimizer{}, "output_budget", "1.0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.opt.Name() != tc.name {
				t.Fatalf("expected name %q, got %q", tc.name, tc.opt.Name())
			}
			if tc.opt.Version() != tc.version {
				t.Fatalf("expected version %q, got %q", tc.version, tc.opt.Version())
			}
		})
	}
}

// --- LossClass constants ---

func TestLossClassConstants(t *testing.T) {
	if Lossless != "lossless" {
		t.Fatalf("expected lossless, got %q", Lossless)
	}
	if Selective != "selective" {
		t.Fatalf("expected selective, got %q", Selective)
	}
	if Lossy != "lossy" {
		t.Fatalf("expected lossy, got %q", Lossy)
	}
}

// --- ActionKind constants ---

func TestActionKindConstants(t *testing.T) {
	if ActionANSIStripped != "ansi_stripped" {
		t.Fatalf("expected ansi_stripped, got %q", ActionANSIStripped)
	}
	if ActionJSONCompacted != "json_compacted" {
		t.Fatalf("expected json_compacted, got %q", ActionJSONCompacted)
	}
	if ActionDuplicateRemoved != "duplicate_removed" {
		t.Fatalf("expected duplicate_removed, got %q", ActionDuplicateRemoved)
	}
	if ActionLogCompacted != "log_compacted" {
		t.Fatalf("expected log_compacted, got %q", ActionLogCompacted)
	}
	if ActionConversationTrimmed != "conversation_trimmed" {
		t.Fatalf("expected conversation_trimmed, got %q", ActionConversationTrimmed)
	}
	if ActionOutputBudget != "output_budget" {
		t.Fatalf("expected output_budget, got %q", ActionOutputBudget)
	}
	if ActionSemanticCompressed != "semantic_compressed" {
		t.Fatalf("expected semantic_compressed, got %q", ActionSemanticCompressed)
	}
	if ActionSemanticCompressionShadow != "semantic_compression_shadow_evaluated" {
		t.Fatalf("expected semantic_compression_shadow_evaluated, got %q", ActionSemanticCompressionShadow)
	}
	if ActionLUIRendered != "lui_rendered" {
		t.Fatalf("expected lui_rendered, got %q", ActionLUIRendered)
	}
	if ActionBypassed != "bypassed" {
		t.Fatalf("expected bypassed, got %q", ActionBypassed)
	}
	if ActionPromptPrefixStabilized != "prompt_prefix_stabilized" {
		t.Fatalf("expected prompt_prefix_stabilized, got %q", ActionPromptPrefixStabilized)
	}
	if ActionToolResultCompacted != "tool_result_compacted" {
		t.Fatalf("expected tool_result_compacted, got %q", ActionToolResultCompacted)
	}
}

// --- StreamFinalizer tests (Section 13) ---

func TestStreamFinalizer_FinalizesExactlyOnce(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-final"}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordComplete, 100, 200, 0)
	if rec.ProviderInputTokensActual != 100 {
		t.Fatalf("expected 100 input actuals, got %d", rec.ProviderInputTokensActual)
	}
	if rec.ProviderOutputTokensActual != 200 {
		t.Fatalf("expected 200 output actuals, got %d", rec.ProviderOutputTokensActual)
	}
	if rec.Status != RecordComplete {
		t.Fatalf("expected status complete, got %q", rec.Status)
	}

	sf.Finalize(context.Background(), RecordComplete, 999, 999, 999)
	if rec.ProviderInputTokensActual != 100 {
		t.Fatalf("expected original value preserved after second finalize, got %d", rec.ProviderInputTokensActual)
	}
}

func TestStreamFinalizer_NilRecordNoPanic(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	sf := NewStreamFinalizer(eng, nil)
	sf.Finalize(context.Background(), RecordComplete, 100, 200, 0)
}

func TestStreamFinalizer_NilEngineNoPanic(t *testing.T) {
	rec := &Record{RequestID: "req-1"}
	sf := NewStreamFinalizer(nil, rec)
	sf.Finalize(context.Background(), RecordComplete, 100, 200, 0)
	if rec.ProviderInputTokensActual != 0 {
		t.Fatalf("expected no actuals set with nil engine, got %d", rec.ProviderInputTokensActual)
	}
}

func TestStreamFinalizer_ClientDisconnectStatus(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-disc"}
	sf := NewStreamFinalizer(eng, rec)
	sf.Finalize(context.Background(), RecordIncompleteClientDisconnect, -1, -1, -1)
	if rec.Status != RecordIncompleteClientDisconnect {
		t.Fatalf("expected status incomplete_client_disconnect, got %q", rec.Status)
	}
}

func TestRecordStatusConstants(t *testing.T) {
	if RecordPending != "pending" {
		t.Fatalf("expected pending, got %q", RecordPending)
	}
	if RecordComplete != "complete" {
		t.Fatalf("expected complete, got %q", RecordComplete)
	}
	if RecordIncompleteClientDisconnect != "incomplete_client_disconnect" {
		t.Fatalf("expected incomplete_client_disconnect, got %q", RecordIncompleteClientDisconnect)
	}
	if RecordIncompleteUpstreamError != "incomplete_upstream_error" {
		t.Fatalf("expected incomplete_upstream_error, got %q", RecordIncompleteUpstreamError)
	}
	if RecordCancelled != "cancelled" {
		t.Fatalf("expected cancelled, got %q", RecordCancelled)
	}
}

// --- Conversation optimizer tests (Section 9) ---

func longConversationRequest() *normalization.NormalizedRequest {
	var msgs []normalization.Message
	for i := 0; i < 40; i++ {
		msgs = append(msgs, normalization.Message{
			Role: normalization.RoleUser,
			Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: "user question " + itoa(i) + " " + strings.Repeat("x", 200)},
			},
		})
		msgs = append(msgs, normalization.Message{
			Role: normalization.RoleAssistant,
			Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: "assistant answer " + itoa(i) + " " + strings.Repeat("y", 200)},
			},
		})
	}
	return &normalization.NormalizedRequest{
		Messages: msgs,
	}
}

func TestConversationOptimizer_TrimmedOldHistory(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Conversation.Enabled = true
	cfg.Conversation.TriggerTokens = 10
	cfg.Conversation.TargetTokens = 100
	cfg.Conversation.RecentTurnsFull = 2
	eng := NewEngine(cfg, nil, nullLogger())
	req := longConversationRequest()
	oc := OptimizationContext{}
	_, res, _, err := eng.Process(context.Background(), req, oc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Messages) >= 80 {
		t.Fatalf("expected messages to be trimmed, got %d", len(req.Messages))
	}
	found := false
	for _, a := range res.Actions {
		if a.Kind == ActionConversationTrimmed {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected conversation trimmed action")
	}
}

func TestConversationOptimizer_OnlyLastUserProtected(t *testing.T) {
	msgs := []normalization.Message{
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("a", 200)}}},
		{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("b", 200)}}},
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("c", 200)}}},
		{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("d", 200)}}},
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("e", 200)}}},
	}
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Conversation.Enabled = true
	cfg.Conversation.TriggerTokens = 10
	cfg.Conversation.TargetTokens = 100
	cfg.Conversation.RecentTurnsFull = 1
	eng := NewEngine(cfg, nil, nullLogger())
	req := &normalization.NormalizedRequest{Messages: msgs}
	oc := OptimizationContext{}
	_, _, _, err := eng.Process(context.Background(), req, oc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundLastUser := false
	for _, m := range req.Messages {
		if m.Role == normalization.RoleUser && m.Content[0].Text == strings.Repeat("e", 200) {
			foundLastUser = true
		}
	}
	if !foundLastUser {
		t.Fatal("expected last user message to be retained")
	}
}

func TestConversationOptimizer_ToolPairsPreserved(t *testing.T) {
	msgs := []normalization.Message{
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("q", 200)}}},
		{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{
			{Type: normalization.ContentToolCall, ToolCallID: "call_1", ToolName: "search"},
		}},
		{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
			{Type: normalization.ContentToolResult, ToolCallID: "call_1", Text: strings.Repeat("result", 100)},
		}},
		{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("a", 200)}}},
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("final", 200)}}},
	}
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Conversation.Enabled = true
	cfg.Conversation.TriggerTokens = 10
	cfg.Conversation.TargetTokens = 50
	cfg.Conversation.RecentTurnsFull = 1
	eng := NewEngine(cfg, nil, nullLogger())
	req := &normalization.NormalizedRequest{Messages: msgs}
	oc := OptimizationContext{}
	_, _, _, err := eng.Process(context.Background(), req, oc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, m := range req.Messages {
		for _, blk := range m.Content {
			if blk.Type == normalization.ContentToolResult && blk.ToolCallID == "call_1" {
				found := false
				for _, m2 := range req.Messages {
					for _, blk2 := range m2.Content {
						if blk2.Type == normalization.ContentToolCall && blk2.ToolCallID == "call_1" {
							found = true
						}
					}
				}
				if !found {
					t.Fatal("expected tool result to be removed with its tool call, or both retained")
				}
				return
			}
		}
	}
}

func TestConversationOptimizer_NoTrimBelowTrigger(t *testing.T) {
	msgs := []normalization.Message{
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "hi"}}},
	}
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Conversation.Enabled = true
	cfg.Conversation.TriggerTokens = 999999
	cfg.Conversation.TargetTokens = 100
	cfg.Conversation.RecentTurnsFull = 2
	eng := NewEngine(cfg, nil, nullLogger())
	req := &normalization.NormalizedRequest{Messages: msgs}
	oc := OptimizationContext{}
	_, res, _, err := eng.Process(context.Background(), req, oc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected no trimming, got %d messages", len(req.Messages))
	}
	for _, a := range res.Actions {
		if a.Kind == ActionConversationTrimmed {
			t.Fatal("expected no conversation trimmed action")
		}
	}
}

func TestConversationOptimizer_SystemPreserved(t *testing.T) {
	msgs := []normalization.Message{
		{Role: normalization.RoleSystem, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "system instructions"}}},
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("q", 200)}}},
		{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("a", 200)}}},
		{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("q2", 200)}}},
		{Role: normalization.RoleAssistant, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: strings.Repeat("a2", 200)}}},
	}
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Conversation.Enabled = true
	cfg.Conversation.TriggerTokens = 10
	cfg.Conversation.TargetTokens = 50
	cfg.Conversation.RecentTurnsFull = 1
	eng := NewEngine(cfg, nil, nullLogger())
	req := &normalization.NormalizedRequest{Messages: msgs}
	oc := OptimizationContext{}
	_, _, _, err := eng.Process(context.Background(), req, oc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Messages[0].Role != normalization.RoleSystem {
		t.Fatal("expected system message to be preserved")
	}
}

func TestConversationOptimizer_EmptyMessages(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Conversation.Enabled = true
	cfg.Conversation.TriggerTokens = 10
	cfg.Conversation.TargetTokens = 50
	cfg.Conversation.RecentTurnsFull = 1
	eng := NewEngine(cfg, nil, nullLogger())
	req := &normalization.NormalizedRequest{Messages: []normalization.Message{}}
	oc := OptimizationContext{}
	_, _, _, err := eng.Process(context.Background(), req, oc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Messages) != 0 {
		t.Fatalf("expected no change, got %d messages", len(req.Messages))
	}
}

// --- Pending optimization-record state tests (HIGH-6) ---

func TestRecordFromResult_CreatedPending(t *testing.T) {
	oc := defaultOC()
	res := &OptimizationResult{
		ModeApplied: config.OptModeSafe,
		LossClass:   Lossless,
	}
	rec := RecordFromResult(oc, res)
	if rec.Status != RecordPending {
		t.Fatalf("expected RecordFromResult to create pending record, got %q", rec.Status)
	}
}

func TestStreamFinalizer_NonStreamFinalizesComplete(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-non-stream", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordComplete, 100, 200, 50)
	if rec.Status != RecordComplete {
		t.Fatalf("expected complete after non-stream finalize, got %q", rec.Status)
	}
	if rec.ProviderInputTokensActual != 100 {
		t.Fatalf("expected 100 input actuals, got %d", rec.ProviderInputTokensActual)
	}
	if rec.ProviderOutputTokensActual != 200 {
		t.Fatalf("expected 200 output actuals, got %d", rec.ProviderOutputTokensActual)
	}
	if rec.CacheReadTokensActual != 50 {
		t.Fatalf("expected 50 cache read actuals, got %d", rec.CacheReadTokensActual)
	}
}

func TestStreamFinalizer_StreamEOFBecomesComplete(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-eof", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordComplete, -1, -1, -1)
	if rec.Status != RecordComplete {
		t.Fatalf("expected complete after stream EOF, got %q", rec.Status)
	}
}

func TestStreamFinalizer_DisconnectBecomesIncomplete(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-disc2", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordIncompleteClientDisconnect, -1, -1, -1)
	if rec.Status != RecordIncompleteClientDisconnect {
		t.Fatalf("expected incomplete_client_disconnect, got %q", rec.Status)
	}
}

func TestStreamFinalizer_UpstreamErrorBecomesIncomplete(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-upstream", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordIncompleteUpstreamError, -1, -1, -1)
	if rec.Status != RecordIncompleteUpstreamError {
		t.Fatalf("expected incomplete_upstream_error, got %q", rec.Status)
	}
}

func TestStreamFinalizer_CancellationBecomesCancelled(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-cancel", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordCancelled, -1, -1, -1)
	if rec.Status != RecordCancelled {
		t.Fatalf("expected cancelled, got %q", rec.Status)
	}
}

func TestStreamFinalizer_RepeatedFinalizationIdempotent(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-repeat", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	// First finalization: upstream error
	sf.Finalize(context.Background(), RecordIncompleteUpstreamError, 10, 20, 5)
	if rec.Status != RecordIncompleteUpstreamError {
		t.Fatalf("expected incomplete_upstream_error after first finalize, got %q", rec.Status)
	}
	if rec.ProviderInputTokensActual != 10 {
		t.Fatalf("expected 10 input actuals, got %d", rec.ProviderInputTokensActual)
	}

	// Second finalization: should be ignored
	sf.Finalize(context.Background(), RecordComplete, 999, 999, 999)
	if rec.Status != RecordIncompleteUpstreamError {
		t.Fatalf("expected status unchanged by second finalize, got %q", rec.Status)
	}
	if rec.ProviderInputTokensActual != 10 {
		t.Fatalf("expected input actuals unchanged by second finalize, got %d", rec.ProviderInputTokensActual)
	}
}

func TestStreamFinalizer_PendingOverwriteByFinalize(t *testing.T) {
	// A pending record should transition to a terminal status on Finalize.
	cfg := testOptConfig(true, config.OptModeSafe)
	eng := NewEngine(cfg, nil, nullLogger())
	rec := &Record{RequestID: "req-pend-over", Status: RecordPending}
	sf := NewStreamFinalizer(eng, rec)

	sf.Finalize(context.Background(), RecordComplete, 100, 200, 0)
	if rec.Status != RecordComplete {
		t.Fatalf("expected pending -> complete transition, got %q", rec.Status)
	}
}

// --- HIGH-4 / HIGH-5 tests ---

func TestEngine_SafeModeNoDedupe(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	cfg.Deterministic.Deduplicate = true
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithDuplicateToolResults()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range res.Actions {
		if a.Kind == ActionDuplicateRemoved {
			t.Fatal("deduplication should not run in safe mode")
		}
	}
}

func TestEngine_SafeModeNoLogCompact(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	cfg.Deterministic.CompactLogs = true
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithLogs()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range res.Actions {
		if a.Kind == ActionLogCompacted {
			t.Fatal("log compaction should not run in safe mode")
		}
	}
}

func TestEngine_SafeModeActionsAreLosslessReversible(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeSafe)
	cfg.Deterministic.CompactLogs = false
	eng := NewEngine(cfg, nil, nullLogger())

	req := &normalization.NormalizedRequest{
		Messages: []normalization.Message{
			{Role: normalization.RoleTool, Content: []normalization.ContentBlock{
				{Type: normalization.ContentText, Text: `{"key": "value", "nested": {"a": 1}}`},
			}},
		},
	}
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range res.Actions {
		if !a.Reversible {
			t.Fatalf("safe-mode action %s must be reversible", a.Kind)
		}
		if a.LossClass != Lossless {
			t.Fatalf("safe-mode action %s must be lossless, got %v", a.Kind, a.LossClass)
		}
	}
}

func TestCompactJSON_RejectsInvalidJSON(t *testing.T) {
	tests := []string{
		"not json at all",
		"{unquoted}",
		"[incomplete",
		"'single quotes'",
		"",
	}
	for _, input := range tests {
		result, ok := compactJSON(input)
		if ok {
			t.Errorf("expected compactJSON to return false for invalid JSON %q", input)
		}
		if result != input {
			t.Errorf("expected compactJSON to return input unchanged for invalid JSON, got %q", result)
		}
	}
}

func TestCompactJSON_ValidJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "spaces removed", input: `{"key": "value"}`, expected: `{"key":"value"}`},
		{name: "tabs removed", input: "{\"key\":\t\"value\"}", expected: `{"key":"value"}`},
		{name: "newlines removed", input: "{\n\"key\": \"value\"\n}", expected: `{"key":"value"}`},
		{name: "nested", input: `{"a": {"b": 1}}`, expected: `{"a":{"b":1}}`},
		{name: "strings preserved", input: `{"key": "value with spaces"}`, expected: `{"key":"value with spaces"}`},
		{name: "escaped quotes", input: `{"key": "say \"hello\""}`, expected: `{"key":"say \"hello\""}`},
		{name: "array", input: `[1, 2, 3]`, expected: `[1,2,3]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := compactJSON(tc.input)
			if !ok {
				t.Fatalf("compactJSON returned false for valid JSON %q", tc.input)
			}
			if result != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestEngine_BalancedMayInvokeSelective(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = true
	cfg.Deterministic.CompactLogs = true
	eng := NewEngine(cfg, nil, nullLogger())
	req := requestWithDuplicateToolResults()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hasDedupe := false
	for _, a := range res.Actions {
		if a.Kind == ActionDuplicateRemoved {
			hasDedupe = true
			if a.LossClass != Selective && a.LossClass != Lossless {
				t.Fatalf("expected balanced action to be lossless or selective, got %v", a.LossClass)
			}
		}
	}
	if !hasDedupe {
		t.Fatal("expected balanced mode to invoke deduplication")
	}
}

func TestEngine_AggressiveRequiresAuthorization(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = false
	eng := NewEngine(cfg, nil, nullLogger())
	mi, err := eng.ResolveMode("", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mi.Applied == config.OptModeAggressive {
		t.Fatal("expected aggressive to be clamped when AggressiveAllowed=false")
	}
	if mi.Applied != config.OptModeBalanced {
		t.Fatalf("expected applied balanced when aggressive not authorized, got %v", mi.Applied)
	}

	cfg2 := testOptConfig(true, config.OptModeAggressive)
	cfg2.AggressiveAllowed = true
	eng2 := NewEngine(cfg2, nil, nullLogger())
	mi2, err2 := eng2.ResolveMode("", "")
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if mi2.Applied != config.OptModeAggressive {
		t.Fatalf("expected applied aggressive when authorized, got %v", mi2.Applied)
	}
}

func TestEngine_ClientPreferenceCannotExceedKeyMax(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeAggressive)
	cfg.AggressiveAllowed = true
	req, app, err := ResolveMode(cfg, "aggressive", "safe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req != config.OptModeAggressive {
		t.Fatalf("expected requested aggressive, got %v", req)
	}
	if app != config.OptModeSafe {
		t.Fatalf("expected applied safe (clamped by key max), got %v", app)
	}
}

func TestEngine_KeyMaxCannotExceedServerMax(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	req, app, err := ResolveMode(cfg, "", "aggressive")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app != config.OptModeBalanced {
		t.Fatalf("expected applied balanced (server max prevents aggressive), got %v", app)
	}
	_ = req
}

// --- Semantic compression shadow evaluation (HIGH-2) ---

type mockCompressor struct {
	name       string
	version    string
	onCompress func(ctx context.Context, req CompressionRequest) (*CompressionResponse, error)
}

func (m *mockCompressor) Name() string                              { return m.name }
func (m *mockCompressor) Version(_ context.Context) (string, error) { return m.version, nil }
func (m *mockCompressor) Health(_ context.Context) error            { return nil }
func (m *mockCompressor) Compress(ctx context.Context, req CompressionRequest) (*CompressionResponse, error) {
	return m.onCompress(ctx, req)
}

func requestForShadow() *normalization.NormalizedRequest {
	return &normalization.NormalizedRequest{
		System: "You are a helpful assistant.",
		Messages: []normalization.Message{
			{
				Role: normalization.RoleAssistant,
				Content: []normalization.ContentBlock{
					{Type: normalization.ContentText, Text: "This is a long assistant response that could be compressed. " + strings.Repeat("padding ", 200)},
				},
			},
			{
				Role:    normalization.RoleUser,
				Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "current user request — must never change"}},
			},
		},
	}
}

func enableSemanticCompression(cfg *config.OptimizationConfig) {
	cfg.SemanticCompression.Enabled = true
	cfg.SemanticCompression.Adapter = "test-compressor"
	cfg.SemanticCompression.MinimumInputTokens = 1
	cfg.SemanticCompression.MinimumExpectedSavingsTokens = 0
	cfg.SemanticCompression.FailureMode = "bypass"
}

func TestSemanticCompressionShadow_RequestNotMutated(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = false
	enableSemanticCompression(&cfg)
	eng := NewEngine(cfg, nil, nullLogger())

	eng.SetCompressor("test-compressor", &mockCompressor{
		name: "test-compressor",
		onCompress: func(_ context.Context, req CompressionRequest) (*CompressionResponse, error) {
			return &CompressionResponse{
				Protocol:     CompressorProtocol,
				Text:         "compressed text",
				InputTokens:  len(req.Text),
				OutputTokens: 10,
				LossClass:    "lossy",
				Model:        "test-model",
			}, nil
		},
	})

	original := requestForShadow()
	origBytes, _ := json.Marshal(original)

	_, res, _, err := eng.Process(context.Background(), original, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Byte-for-byte: request must not be mutated.
	afterBytes, _ := json.Marshal(original)
	if string(origBytes) != string(afterBytes) {
		t.Fatal("shadow semantic compression mutated the request")
	}

	// Current user content must be byte-identical.
	if len(original.Messages) < 2 {
		t.Fatal("expected at least 2 messages")
	}
	last := original.Messages[len(original.Messages)-1]
	if last.Role != normalization.RoleUser {
		t.Fatal("last message should be user")
	}
	if last.Content[0].Text != "current user request — must never change" {
		t.Fatal("current user content was mutated")
	}

	// Must not record ActionSemanticCompressed.
	for _, a := range res.Actions {
		if a.Kind == ActionSemanticCompressed {
			t.Fatal("shadow evaluation must not record ActionSemanticCompressed")
		}
	}

	// Must record the shadow action.
	foundShadow := false
	for _, a := range res.Actions {
		if a.Kind == ActionSemanticCompressionShadow {
			foundShadow = true
			break
		}
	}
	if !foundShadow {
		t.Fatal("shadow evaluation did not record ActionSemanticCompressionShadow")
	}

	if !res.ShadowEvaluated {
		t.Fatal("expected ShadowEvaluated=true")
	}
	if res.HypotheticalSavingsTokens <= 0 {
		t.Fatal("expected positive HypotheticalSavingsTokens")
	}
}

func TestSemanticCompressionShadow_SavingsNotRealized(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = false
	enableSemanticCompression(&cfg)
	eng := NewEngine(cfg, nil, nullLogger())

	eng.SetCompressor("test-compressor", &mockCompressor{
		name: "test-compressor",
		onCompress: func(_ context.Context, req CompressionRequest) (*CompressionResponse, error) {
			return &CompressionResponse{
				Protocol:     CompressorProtocol,
				Text:         "compressed text",
				InputTokens:  len(req.Text),
				OutputTokens: 10,
				LossClass:    "lossy",
				Model:        "test-model",
			}, nil
		},
	})

	req := requestForShadow()
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Hypothetical savings must be positive.
	if res.HypotheticalSavingsTokens <= 0 {
		t.Fatal("expected positive HypotheticalSavingsTokens")
	}

	// Realized savings (RemovedTokensEstimated) must not include hypothetical savings.
	// Since the request is not mutated, there should be no removed tokens from compression.
	if res.RemovedTokensEstimated < 0 {
		t.Fatal("RemovedTokensEstimated must not be negative")
	}

	// CompressionTokens must remain 0 (not set by shadow evaluation).
	if res.CompressionTokens != 0 {
		t.Fatal("shadow evaluation must not set CompressionTokens")
	}
}

func TestSemanticCompressionShadow_FailureBypass(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = false
	enableSemanticCompression(&cfg)
	cfg.SemanticCompression.FailureMode = "bypass"
	eng := NewEngine(cfg, nil, nullLogger())

	eng.SetCompressor("test-compressor", &mockCompressor{
		name: "test-compressor",
		onCompress: func(_ context.Context, _ CompressionRequest) (*CompressionResponse, error) {
			return nil, fmt.Errorf("compressor unavailable")
		},
	})

	req := requestForShadow()
	origBytes, _ := json.Marshal(req)

	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("bypass mode must not propagate error: %v", err)
	}

	// Request must still be unchanged.
	afterBytes, _ := json.Marshal(req)
	if string(origBytes) != string(afterBytes) {
		t.Fatal("compressor failure in bypass mode mutated the request")
	}

	// No shadow action should be recorded.
	for _, a := range res.Actions {
		if a.Kind == ActionSemanticCompressionShadow || a.Kind == ActionSemanticCompressed {
			t.Fatal("compressor failure must not record any semantic compression action")
		}
	}

	if res.ShadowEvaluated {
		t.Fatal("compressor failure must not set ShadowEvaluated")
	}

	// Warning should explain the bypass.
	foundWarn := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "semantic compression bypassed") {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Fatal("expected bypass warning in bypass mode")
	}
}

func TestSemanticCompressionShadow_FailureReject(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	cfg.Deterministic.Deduplicate = false
	enableSemanticCompression(&cfg)
	cfg.SemanticCompression.FailureMode = "reject"
	eng := NewEngine(cfg, nil, nullLogger())

	eng.SetCompressor("test-compressor", &mockCompressor{
		name: "test-compressor",
		onCompress: func(_ context.Context, _ CompressionRequest) (*CompressionResponse, error) {
			return nil, fmt.Errorf("compressor unavailable")
		},
	})

	req := requestForShadow()
	origBytes, _ := json.Marshal(req)

	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("reject mode must not propagate engine error: %v", err)
	}

	// Request must still be unchanged.
	afterBytes, _ := json.Marshal(req)
	if string(origBytes) != string(afterBytes) {
		t.Fatal("compressor failure in reject mode mutated the request")
	}

	// No shadow action should be recorded.
	for _, a := range res.Actions {
		if a.Kind == ActionSemanticCompressionShadow || a.Kind == ActionSemanticCompressed {
			t.Fatal("compressor failure must not record any semantic compression action")
		}
	}

	if res.ShadowEvaluated {
		t.Fatal("compressor failure must not set ShadowEvaluated")
	}

	// Warning should say "rejected".
	foundWarn := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "semantic compression rejected") {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Fatal("expected rejection warning in reject mode")
	}
}

func TestSemanticCompressionShadow_SingleMessageNoEligible(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	enableSemanticCompression(&cfg)
	eng := NewEngine(cfg, nil, nullLogger())

	eng.SetCompressor("test-compressor", &mockCompressor{
		name: "test-compressor",
		onCompress: func(_ context.Context, _ CompressionRequest) (*CompressionResponse, error) {
			t.Fatal("compressor should not be called with no eligible content")
			return nil, nil
		},
	})

	req := &normalization.NormalizedRequest{
		System: "system only",
		Messages: []normalization.Message{
			{Role: normalization.RoleUser, Content: []normalization.ContentBlock{{Type: normalization.ContentText, Text: "only message"}}},
		},
	}

	origBytes, _ := json.Marshal(req)
	_, res, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	afterBytes, _ := json.Marshal(req)
	if string(origBytes) != string(afterBytes) {
		t.Fatal("request mutated despite no eligible content")
	}

	if res.ShadowEvaluated {
		t.Fatal("ShadowEvaluated must be false with no eligible content")
	}
	for _, a := range res.Actions {
		if a.Kind == ActionSemanticCompressionShadow || a.Kind == ActionSemanticCompressed {
			t.Fatal("no action expected with no eligible content")
		}
	}
}

func TestSemanticCompressionShadow_UserContentPreserved(t *testing.T) {
	cfg := testOptConfig(true, config.OptModeBalanced)
	enableSemanticCompression(&cfg)
	eng := NewEngine(cfg, nil, nullLogger())

	eng.SetCompressor("test-compressor", &mockCompressor{
		name: "test-compressor",
		onCompress: func(_ context.Context, req CompressionRequest) (*CompressionResponse, error) {
			return &CompressionResponse{
				Protocol:     CompressorProtocol,
				Text:         "compressed " + req.Text,
				InputTokens:  len(req.Text),
				OutputTokens: 10,
				LossClass:    "lossy",
				Model:        "test-model",
			}, nil
		},
	})

	req := requestForShadow()
	userMsgIdx := -1
	for i, m := range req.Messages {
		if m.Role == normalization.RoleUser {
			userMsgIdx = i
			break
		}
	}
	if userMsgIdx < 0 {
		t.Fatal("expected a user message")
	}
	userText := req.Messages[userMsgIdx].Content[0].Text

	_, _, _, err := eng.Process(context.Background(), req, defaultOC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Messages[userMsgIdx].Content[0].Text != userText {
		t.Fatal("user message content was mutated by shadow evaluation")
	}
}

// --- HIGH-3: Compressor request/response limit enforcement ---

func startTestCompressorHandler(t *testing.T, handler http.HandlerFunc) (*httptest.Server, config.CompressorConfig) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.CompressorConfig{
		Enabled:          true,
		Transport:        "http",
		Endpoint:         srv.URL,
		MaxRequestBytes:  8 << 20,
		MaxResponseBytes: 1024,
		AllowNonLoopback: true,
	}
	return srv, cfg
}

func TestCompressorResponse_ExactlyAtLimitSucceeds(t *testing.T) {
	payload := strings.Repeat("x", 1024)
	_, cfg := startTestCompressorHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(payload))
	})
	comp := newHTTPCompressor("test", cfg)
	resp, err := comp.call(context.Background(), CompressionRequest{ContentClass: "test"})
	if err != nil {
		t.Fatalf("expected success for exactly-at-limit response, got: %v", err)
	}
	if len(resp) != 1024 {
		t.Fatalf("expected 1024 bytes, got %d", len(resp))
	}
}

func TestCompressorResponse_OneByteOverFails(t *testing.T) {
	payload := strings.Repeat("x", 1025)
	_, cfg := startTestCompressorHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(payload))
	})
	comp := newHTTPCompressor("test", cfg)
	_, err := comp.call(context.Background(), CompressionRequest{ContentClass: "test"})
	if err == nil {
		t.Fatal("expected error for one-byte-over response")
	}
	if !errors.Is(err, ErrCompressorResponseTooLarge) {
		t.Fatalf("expected ErrCompressorResponseTooLarge, got: %v", err)
	}
}

func TestCompressorResponse_ChunkedOversizedFails(t *testing.T) {
	_, cfg := startTestCompressorHandler(t, func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.Repeat("a", 512)))
		flusher.Flush()
		w.Write([]byte(strings.Repeat("b", 514)))
		flusher.Flush()
	})
	comp := newHTTPCompressor("test", cfg)
	_, err := comp.call(context.Background(), CompressionRequest{ContentClass: "test"})
	if err == nil {
		t.Fatal("expected error for chunked oversized response")
	}
	if !errors.Is(err, ErrCompressorResponseTooLarge) {
		t.Fatalf("expected ErrCompressorResponseTooLarge, got: %v", err)
	}
}

func TestCompressor_RedirectToRestrictedAddressFails(t *testing.T) {
	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example.com/malware", http.StatusFound)
	}))
	t.Cleanup(redirectSrv.Close)
	cfg := config.CompressorConfig{
		Enabled:          true,
		Transport:        "http",
		Endpoint:         redirectSrv.URL,
		MaxRequestBytes:  8 << 20,
		MaxResponseBytes: 1024,
		AllowNonLoopback: false,
	}
	comp := newHTTPCompressor("test-redirect", cfg)
	_, err := comp.call(context.Background(), CompressionRequest{ContentClass: "test"})
	if err == nil {
		t.Fatal("expected error for redirect")
	}
}

func TestCompressor_URLUserinfoFails(t *testing.T) {
	cfg := config.CompressorConfig{
		Enabled:          true,
		Transport:        "http",
		Endpoint:         "http://user:pass@127.0.0.1:99999/compress",
		MaxRequestBytes:  8 << 20,
		MaxResponseBytes: 1024,
		AllowNonLoopback: true,
	}
	comp := newHTTPCompressor("test-userinfo", cfg)
	_, err := comp.call(context.Background(), CompressionRequest{ContentClass: "test"})
	if err == nil {
		t.Fatal("expected error for URL with userinfo")
	}
}

func TestCompressor_RequestBodyExceedsLimitRejected(t *testing.T) {
	_, cfg := startTestCompressorHandler(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	cfg.MaxRequestBytes = 16
	comp := newHTTPCompressor("test-reqlimit", cfg)
	req := CompressionRequest{
		ContentClass: "test",
		Text:         strings.Repeat("x", 100),
	}
	_, err := comp.call(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for request body exceeding max_request_bytes")
	}
}
