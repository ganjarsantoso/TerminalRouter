package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/api/middleware"
	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/storage"
)

// Gateway dependencies for OpenAI-compatible endpoints.
type Gateway struct {
	Resolver       *router.Resolver
	Coordinator    *execution.Coordinator
	Store          *storage.Store
	Log            *observability.Logger
	AllowDirect    bool
	RequestTimeout time.Duration
}

func (g *Gateway) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInvalidRequest, "method not allowed", 405))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInvalidRequest, "failed to read body", 400))
		return
	}
	nreq, err := ParseChatRequest(body)
	if err != nil {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInvalidRequest, err.Error(), 400))
		return
	}
	nreq.ID = observability.RequestIDFrom(r.Context())

	if ck := middleware.ClientKeyFrom(r.Context()); ck != nil {
		if !ck.AliasAllowed(nreq.RequestedModel) && !strings.Contains(nreq.RequestedModel, "/") {
			middleware.WriteError(w, r, normalization.NewError(normalization.ErrPermissionDenied,
				fmt.Sprintf("client key is not allowed to use model %q", nreq.RequestedModel), 403))
			return
		}
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

	start := time.Now()
	if nreq.Stream {
		g.handleStream(w, r, nreq, plan, start)
		return
	}

	result, err := g.Coordinator.Execute(ctx, nreq, plan)
	lat := time.Since(start)
	if err != nil {
		ne := toNormErr(err)
		g.logRequest(r, nreq, plan, "", 0, lat, ne, false)
		middleware.WriteError(w, r, ne)
		return
	}
	g.logRequest(r, nreq, plan, result.ProviderID, result.Attempt, lat, nil, false)
	writeChatResponse(w, result.Response)
}

func (g *Gateway) handleStream(w http.ResponseWriter, r *http.Request, nreq *normalization.NormalizedRequest, plan *router.Plan, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		middleware.WriteError(w, r, normalization.NewError(normalization.ErrInternal, "streaming not supported", 500))
		return
	}
	sr, err := g.Coordinator.ExecuteStream(r.Context(), nreq, plan)
	if err != nil {
		ne := toNormErr(err)
		g.logRequest(r, nreq, plan, "", 0, time.Since(start), ne, true)
		middleware.WriteError(w, r, ne)
		return
	}
	defer sr.Stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	id := nreq.ID
	if id == "" {
		id = "chatcmpl-" + observability.RequestIDFrom(r.Context())
	}
	model := plan.PublicModel
	var usage *normalization.Usage
	var finish string
	ttft := 0
	first := true

	for {
		ev, err := sr.Stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			// After commit we cannot failover; emit error and end
			writeSSE(w, map[string]any{
				"error": map[string]any{"message": err.Error(), "type": "server_error"},
			})
			flusher.Flush()
			g.logRequest(r, nreq, plan, sr.ProviderID, sr.Attempt, time.Since(start), toNormErr(err), true)
			return
		}
		if ev.Commit && first {
			ttft = int(time.Since(start).Milliseconds())
			first = false
		}
		switch ev.Type {
		case normalization.EventTextDelta:
			chunk := streamChunk(id, model, map[string]any{
				"content": ev.Text,
			}, nil)
			writeSSE(w, chunk)
			flusher.Flush()
		case normalization.EventToolCallStart:
			delta := map[string]any{
				"tool_calls": []map[string]any{{
					"index": ev.Index,
					"id":    ev.ToolCallID,
					"type":  "function",
					"function": map[string]any{
						"name":      ev.ToolName,
						"arguments": "",
					},
				}},
			}
			writeSSE(w, streamChunk(id, model, delta, nil))
			flusher.Flush()
		case normalization.EventToolCallDelta:
			delta := map[string]any{
				"tool_calls": []map[string]any{{
					"index": ev.Index,
					"function": map[string]any{
						"arguments": ev.Arguments,
					},
				}},
			}
			writeSSE(w, streamChunk(id, model, delta, nil))
			flusher.Flush()
		case normalization.EventUsageDelta:
			usage = ev.Usage
		case normalization.EventMessageStop:
			finish = normalization.MapStopToOpenAI(ev.StopReason)
			if ev.Usage != nil {
				usage = ev.Usage
			}
		case normalization.EventError:
			if ev.Error != nil {
				writeSSE(w, map[string]any{"error": map[string]any{"message": ev.Error.Message, "type": ev.Error.Code}})
				flusher.Flush()
			}
		}
	}
	// final chunk with finish_reason
	fr := finish
	if fr == "" {
		fr = "stop"
	}
	writeSSE(w, streamChunk(id, model, map[string]any{}, &fr))
	if usage != nil {
		writeSSE(w, map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{},
			"usage": map[string]any{
				"prompt_tokens":     usage.InputTokens,
				"completion_tokens": usage.OutputTokens,
				"total_tokens":      usage.InputTokens + usage.OutputTokens,
			},
		})
	}
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	var ne *normalization.Error
	rec := storage.RequestRecord{
		TimeToFirstTokenMs: ttft,
	}
	_ = rec
	g.logRequest(r, nreq, plan, sr.ProviderID, sr.Attempt, time.Since(start), ne, true)
	if usage != nil {
		// re-log with tokens — simplified: insert once with tokens
	}
}

func (g *Gateway) ListModels(w http.ResponseWriter, r *http.Request) {
	names := g.Resolver.ListPublicModels()
	data := make([]map[string]any, 0, len(names))
	for _, n := range names {
		data = append(data, map[string]any{
			"id":       n,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "termrouter",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
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
	rec := storage.RequestRecord{
		ID:              nreq.ID,
		Timestamp:       time.Now().UTC(),
		ClientKeyID:     ckID,
		InboundProtocol: "openai",
		RequestedModel:  nreq.RequestedModel,
		ResolvedAlias:   plan.Alias,
		ProviderID:      providerID,
		Attempt:         attempt,
		StatusCode:      status,
		LatencyMs:       int(lat.Milliseconds()),
		ErrorClass:      errClass,
		Stream:          stream,
	}
	if nreq.ID == "" {
		rec.ID = observability.RequestIDFrom(r.Context())
	}
	_ = g.Store.InsertRequest(r.Context(), rec)
	if g.Log != nil {
		g.Log.Info("request",
			"request_id", rec.ID,
			"protocol", "openai",
			"model", rec.RequestedModel,
			"alias", rec.ResolvedAlias,
			"provider", providerID,
			"status", status,
			"latency_ms", rec.LatencyMs,
			"stream", stream,
			"error_class", errClass,
		)
	}
}

func writeChatResponse(w http.ResponseWriter, resp *normalization.NormalizedResponse) {
	var content any
	var toolCalls []map[string]any
	var textParts []string
	for _, c := range resp.Content {
		switch c.Type {
		case normalization.ContentText:
			textParts = append(textParts, c.Text)
		case normalization.ContentToolCall:
			toolCalls = append(toolCalls, map[string]any{
				"id":   c.ToolCallID,
				"type": "function",
				"function": map[string]any{
					"name":      c.ToolName,
					"arguments": c.Arguments,
				},
			})
		}
	}
	if len(textParts) > 0 {
		content = strings.Join(textParts, "")
	} else {
		content = nil
	}
	msg := map[string]any{"role": "assistant", "content": content}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	out := map[string]any{
		"id":      resp.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   resp.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       msg,
			"finish_reason": normalization.MapStopToOpenAI(resp.StopReason),
		}},
		"usage": map[string]any{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func streamChunk(id, model string, delta map[string]any, finish *string) map[string]any {
	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finish != nil {
		choice["finish_reason"] = *finish
		choice["delta"] = map[string]any{}
	} else {
		choice["finish_reason"] = nil
	}
	return map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{choice},
	}
}

func writeSSE(w http.ResponseWriter, v any) {
	b, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", b)
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

// ParseChatRequest converts OpenAI chat completion JSON to NormalizedRequest.
func ParseChatRequest(body []byte) (*normalization.NormalizedRequest, error) {
	var raw struct {
		Model            string          `json:"model"`
		Messages         []chatMessage   `json:"messages"`
		Stream           bool            `json:"stream"`
		Temperature      *float64        `json:"temperature"`
		TopP             *float64        `json:"top_p"`
		MaxTokens        *int            `json:"max_tokens"`
		MaxCompletionTok *int            `json:"max_completion_tokens"`
		Stop             json.RawMessage `json:"stop"`
		Tools            []struct {
			Type     string `json:"type"`
			Function struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				Parameters  map[string]any `json:"parameters"`
			} `json:"function"`
		} `json:"tools"`
		ToolChoice     json.RawMessage `json:"tool_choice"`
		ResponseFormat map[string]any  `json:"response_format"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if raw.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if len(raw.Messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}
	nreq := &normalization.NormalizedRequest{
		RequestedModel: raw.Model,
		Stream:         raw.Stream,
		Temperature:    raw.Temperature,
		TopP:           raw.TopP,
		ResponseFormat: raw.ResponseFormat,
	}
	if raw.MaxCompletionTok != nil {
		nreq.MaxOutputTokens = raw.MaxCompletionTok
	} else if raw.MaxTokens != nil {
		nreq.MaxOutputTokens = raw.MaxTokens
	}
	if len(raw.Stop) > 0 {
		var one string
		var many []string
		if err := json.Unmarshal(raw.Stop, &one); err == nil {
			nreq.StopSequences = []string{one}
		} else if err := json.Unmarshal(raw.Stop, &many); err == nil {
			nreq.StopSequences = many
		}
	}
	for _, m := range raw.Messages {
		nm, sys := convertMessage(m)
		if sys != "" {
			nreq.System += sys
		}
		if nm != nil {
			nreq.Messages = append(nreq.Messages, *nm)
		}
	}
	for _, t := range raw.Tools {
		nreq.Tools = append(nreq.Tools, normalization.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	if len(nreq.Tools) > 0 {
		nreq.RequiredCapabilities = append(nreq.RequiredCapabilities, "tools")
	}
	if len(raw.ToolChoice) > 0 {
		var s string
		if err := json.Unmarshal(raw.ToolChoice, &s); err == nil {
			nreq.ToolChoice = &normalization.ToolChoice{Type: s}
		} else {
			var obj struct {
				Type     string `json:"type"`
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(raw.ToolChoice, &obj); err == nil {
				nreq.ToolChoice = &normalization.ToolChoice{Type: "tool", Name: obj.Function.Name}
			}
		}
	}
	return nreq, nil
}

type chatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func convertMessage(m chatMessage) (*normalization.Message, string) {
	role := normalization.Role(m.Role)
	if role == normalization.RoleSystem {
		text := parseContentText(m.Content)
		return nil, text
	}
	msg := &normalization.Message{Role: role, Name: m.Name}
	if role == normalization.RoleTool {
		msg.Content = []normalization.ContentBlock{{
			Type:       normalization.ContentToolResult,
			Text:       parseContentText(m.Content),
			ToolCallID: m.ToolCallID,
		}}
		return msg, ""
	}
	text := parseContentText(m.Content)
	if text != "" {
		msg.Content = append(msg.Content, normalization.ContentBlock{Type: normalization.ContentText, Text: text})
	}
	for _, tc := range m.ToolCalls {
		msg.Content = append(msg.Content, normalization.ContentBlock{
			Type:       normalization.ContentToolCall,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			Arguments:  tc.Function.Arguments,
		})
	}
	return msg, ""
}

func parseContentText(raw json.RawMessage) string {
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
			if p.Type == "text" || p.Type == "" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}
