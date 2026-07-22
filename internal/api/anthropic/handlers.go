package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/api/middleware"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/optimization"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/smart"
	"github.com/termrouter/termrouter/internal/storage"
)

// Gateway serves Anthropic Messages compatibility.
type Gateway struct {
	Resolver       *router.Resolver
	Coordinator    *execution.Coordinator
	Store          *storage.Store
	Log            *observability.Logger
	Cfg            *config.Config
	Smart          *smart.Engine
	AllowDirect    bool
	RequestTimeout time.Duration
}

func (g *Gateway) Messages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInvalidRequest, "method not allowed", 405))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			middleware.WriteError(w, r, normalization.NewError(normalization.ErrRequestTooLarge,
				"request body exceeds the configured limit", 413))
			return
		}
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInvalidRequest, "failed to read body", 400))
		return
	}
	nreq, err := ParseMessagesRequest(body)
	if err != nil {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInvalidRequest, err.Error(), 400))
		return
	}
	nreq.ID = observability.RequestIDFrom(r.Context())

	ck := middleware.ClientKeyFrom(r.Context())
	isAlias := g.Resolver.IsAlias(nreq.RequestedModel)
	if err := middleware.AuthorizeModel(ck, nreq.RequestedModel, isAlias, g.AllowDirect); err != nil {
		if g.Log != nil {
			g.Log.Warn("security_event",
				"event", "forbidden_route",
				"key_id", keyID(ck),
				"model", nreq.RequestedModel,
				"path", r.URL.Path,
				"client_ip", middleware.ClientIPFrom(r.Context()),
			)
		}
		middleware.WriteError(w, r, err)
		return
	}
	if err := middleware.ApplyRequestPolicy(nreq, g.Cfg, ck); err != nil {
		middleware.WriteError(w, r, err)
		return
	}

	plan, err := g.Resolver.Resolve(nreq.RequestedModel, g.AllowDirect)
	if err != nil {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrModelNotFound, err.Error(), 404))
		return
	}
	nreq.ResolvedAlias = plan.Alias
	if plan.PublicModel != "" {
		nreq.RequestedModel = plan.PublicModel
	}

	ctx := r.Context()
	if g.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.RequestTimeout)
		defer cancel()
	}

	if plan.Strategy == "smart" {
		applied, aerr := smart.ApplySmart(ctx, g.Smart, g.Cfg, g.Store, plan, nreq, r)
		if aerr != nil {
			middleware.WriteError(w, r, normalization.NewError(normalization.ErrProviderUnavailable, aerr.Error(), 503))
			return
		}
		plan = applied.Plan
		if applied.Decision != nil {
			smart.WriteDecisionHeaders(w, applied.Decision)
		}
	}

	start := time.Now()
	if nreq.Stream {
		g.handleStream(ctx, w, r, nreq, plan, start)
		return
	}

	result, err := g.Coordinator.Execute(ctx, nreq, plan, execution.PolicyContext{
		ClientKey:     middleware.ClientKeyFrom(ctx),
		PublicHosting: g.Cfg.PublicHosting.Enabled,
	})
	lat := time.Since(start)
	if err != nil {
		ne := toNormErr(err)
		g.logRequest(r, nreq, plan, "", 0, lat, ne, false)
		middleware.WriteError(w, r, ne)
		return
	}
	g.logRequest(r, nreq, plan, result.ProviderID, result.Attempt, lat, nil, false)
	writeMessagesResponse(w, result.Response)
}

func (g *Gateway) handleStream(ctx context.Context, w http.ResponseWriter, r *http.Request, nreq *normalization.NormalizedRequest, plan *router.Plan, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInternal, "streaming not supported", 500))
		return
	}
	sr, err := g.Coordinator.ExecuteStream(ctx, nreq, plan, execution.PolicyContext{
		ClientKey:     middleware.ClientKeyFrom(ctx),
		PublicHosting: g.Cfg.PublicHosting.Enabled,
	})
	if err != nil {
		ne := toNormErr(err)
		g.logRequest(r, nreq, plan, "", 0, time.Since(start), ne, true)
		middleware.WriteError(w, r, ne)
		return
	}
	var streamUsage *normalization.Usage
	defer sr.Stream.Close()
	defer func() {
		if sr.OptFinalizer != nil {
			inputTokens, outputTokens := -1, -1
			if streamUsage != nil {
				inputTokens = streamUsage.InputTokens
				outputTokens = streamUsage.OutputTokens
			}
			sr.OptFinalizer.Finalize(r.Context(), optimization.RecordComplete, inputTokens, outputTokens, -1)
		}
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	msgID := "msg_" + nreq.ID
	writeSSEEvent(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         plan.PublicModel,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
	flusher.Flush()

	blockOpen := false
	blockIndex := 0
	for {
		ev, err := sr.Stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeSSEEvent(w, "error", map[string]any{
				"type":  "error",
				"error": map[string]any{"type": "api_error", "message": err.Error()},
			})
			flusher.Flush()
			g.logRequest(r, nreq, plan, sr.ProviderID, sr.Attempt, time.Since(start), toNormErr(err), true)
			return
		}
		switch ev.Type {
		case normalization.EventTextDelta:
			if !blockOpen {
				writeSSEEvent(w, "content_block_start", map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "text", "text": ""},
				})
				blockOpen = true
			}
			writeSSEEvent(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "text_delta", "text": ev.Text},
			})
			flusher.Flush()
		case normalization.EventToolCallStart:
			if blockOpen {
				writeSSEEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
				blockIndex++
				blockOpen = false
			}
			writeSSEEvent(w, "content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    ev.ToolCallID,
					"name":  ev.ToolName,
					"input": map[string]any{},
				},
			})
			blockOpen = true
			flusher.Flush()
		case normalization.EventToolCallDelta:
			writeSSEEvent(w, "content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": ev.Arguments},
			})
			flusher.Flush()
		case normalization.EventMessageStop:
			if blockOpen {
				writeSSEEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
			}
			srReason := normalization.MapStopToAnthropic(ev.StopReason)
			usage := map[string]any{"output_tokens": 0}
			if ev.Usage != nil {
				usage["output_tokens"] = ev.Usage.OutputTokens
				streamUsage = ev.Usage
			}
			writeSSEEvent(w, "message_delta", map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": srReason, "stop_sequence": nil},
				"usage": usage,
			})
			writeSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
			flusher.Flush()
		}
	}
	g.logRequest(r, nreq, plan, sr.ProviderID, sr.Attempt, time.Since(start), nil, true)
}

func (g *Gateway) logRequest(r *http.Request, nreq *normalization.NormalizedRequest, plan *router.Plan, providerID string, attempt int, lat time.Duration, ne *normalization.Error, stream bool) {
	if g.Store == nil {
		return
	}
	status := 200
	errClass := ""
	if ne != nil {
		status = ne.HTTPStatus
		errClass = ne.Code
	}
	ckID := ""
	if ck := middleware.ClientKeyFrom(r.Context()); ck != nil {
		ckID = ck.ID
	}
	clientLabel := middleware.ClientLabelFrom(r.Context())
	if clientLabel == "" && nreq.Metadata != nil {
		if v, ok := nreq.Metadata["termrouter_client_name"].(string); ok {
			clientLabel = v
		}
	}
	id := nreq.ID
	if id == "" {
		id = observability.RequestIDFrom(r.Context())
	}
	_ = g.Store.InsertRequest(r.Context(), storage.RequestRecord{
		ID: id, Timestamp: time.Now().UTC(), ClientKeyID: ckID, InboundProtocol: "anthropic",
		RequestedModel: nreq.RequestedModel, ResolvedAlias: plan.Alias, ProviderID: providerID,
		Attempt: attempt, StatusCode: status, LatencyMs: int(lat.Milliseconds()), ErrorClass: errClass, Stream: stream,
		ClientLabel: clientLabel,
	})
}

func keyID(ck *storage.ClientKey) string {
	if ck == nil {
		return ""
	}
	return ck.ID
}

func writeMessagesResponse(w http.ResponseWriter, resp *normalization.NormalizedResponse) {
	content := make([]map[string]any, 0, len(resp.Content))
	for _, c := range resp.Content {
		switch c.Type {
		case normalization.ContentText:
			content = append(content, map[string]any{"type": "text", "text": c.Text})
		case normalization.ContentToolCall:
			var input any
			_ = json.Unmarshal([]byte(c.Arguments), &input)
			if input == nil {
				input = map[string]any{}
			}
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    c.ToolCallID,
				"name":  c.ToolName,
				"input": input,
			})
		}
	}
	out := map[string]any{
		"id":            resp.ID,
		"type":          "message",
		"role":          "assistant",
		"model":         resp.Model,
		"content":       content,
		"stop_reason":   normalization.MapStopToAnthropic(resp.StopReason),
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func writeSSEEvent(w http.ResponseWriter, event string, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

func toNormErr(err error) *normalization.Error {
	if err == nil {
		return nil
	}
	if ne, ok := err.(*normalization.Error); ok {
		return ne
	}
	return normalization.NewError(normalization.ErrInternal, err.Error(), 500)
}

// ParseMessagesRequest converts Anthropic Messages JSON to NormalizedRequest.
func ParseMessagesRequest(body []byte) (*normalization.NormalizedRequest, error) {
	var raw struct {
		Model         string          `json:"model"`
		Messages      []msg           `json:"messages"`
		System        json.RawMessage `json:"system"`
		MaxTokens     int             `json:"max_tokens"`
		Stream        bool            `json:"stream"`
		Temperature   *float64        `json:"temperature"`
		TopP          *float64        `json:"top_p"`
		StopSequences []string        `json:"stop_sequences"`
		Tools         []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
		ToolChoice *struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tool_choice"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if raw.MaxTokens <= 0 {
		return nil, fmt.Errorf("max_tokens is required")
	}
	if len(raw.Messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}
	nreq := &normalization.NormalizedRequest{
		RequestedModel:  raw.Model,
		Stream:          raw.Stream,
		Temperature:     raw.Temperature,
		TopP:            raw.TopP,
		MaxOutputTokens: &raw.MaxTokens,
		StopSequences:   raw.StopSequences,
		System:          parseSystem(raw.System),
	}
	for _, m := range raw.Messages {
		nreq.Messages = append(nreq.Messages, convertMsg(m))
	}
	for _, t := range raw.Tools {
		nreq.Tools = append(nreq.Tools, normalization.Tool{
			Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
		})
	}
	if len(nreq.Tools) > 0 {
		nreq.RequiredCapabilities = append(nreq.RequiredCapabilities, "tools")
	}
	if raw.ToolChoice != nil {
		tc := &normalization.ToolChoice{Type: raw.ToolChoice.Type, Name: raw.ToolChoice.Name}
		if tc.Type == "any" {
			tc.Type = "required"
		}
		nreq.ToolChoice = tc
	}
	return nreq, nil
}

type msg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func parseSystem(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func convertMsg(m msg) normalization.Message {
	role := normalization.Role(m.Role)
	nm := normalization.Message{Role: role}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		nm.Content = []normalization.ContentBlock{{Type: normalization.ContentText, Text: s}}
		return nm
	}
	var parts []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		IsError   bool            `json:"is_error"`
	}
	if err := json.Unmarshal(m.Content, &parts); err != nil {
		return nm
	}
	for _, p := range parts {
		switch p.Type {
		case "text":
			nm.Content = append(nm.Content, normalization.ContentBlock{Type: normalization.ContentText, Text: p.Text})
		case "tool_use":
			nm.Content = append(nm.Content, normalization.ContentBlock{
				Type: normalization.ContentToolCall, ToolCallID: p.ID, ToolName: p.Name, Arguments: string(p.Input),
			})
		case "tool_result":
			text := ""
			var ts string
			if json.Unmarshal(p.Content, &ts) == nil {
				text = ts
			} else {
				text = string(p.Content)
			}
			nm.Role = normalization.RoleTool
			nm.Content = append(nm.Content, normalization.ContentBlock{
				Type: normalization.ContentToolResult, ToolCallID: p.ToolUseID, Text: text, IsError: p.IsError,
			})
		}
	}
	return nm
}
