package compatible

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/provider"
)

// Adapter implements OpenAI and OpenAI-compatible providers.
type Adapter struct {
	// TypeName is "openai" or "openai-compatible".
	TypeName   string
	DefaultURL string
	HTTPClient *http.Client
}

func NewOpenAI() *Adapter {
	return &Adapter{
		TypeName:   "openai",
		DefaultURL: "https://api.openai.com/v1",
		HTTPClient: &http.Client{Timeout: 0}, // per-request context controls timeout
	}
}

func NewCompatible() *Adapter {
	return &Adapter{
		TypeName:   "openai-compatible",
		DefaultURL: "",
		HTTPClient: &http.Client{Timeout: 0},
	}
}

func (a *Adapter) Type() string { return a.TypeName }

func (a *Adapter) Capabilities(conn config.ProviderConfig) provider.CapabilitySet {
	return provider.CapabilitySet{
		Chat: true, Streaming: true, Tools: true, SystemMessage: true, JSONMode: true,
	}
}

func (a *Adapter) baseURL(conn config.ProviderConfig) string {
	if conn.BaseURL != "" {
		return strings.TrimRight(conn.BaseURL, "/")
	}
	return strings.TrimRight(a.DefaultURL, "/")
}

func (a *Adapter) Validate(ctx context.Context, conn config.ProviderConfig, credential string) error {
	models, err := a.ListModels(ctx, conn, credential)
	if err != nil {
		// Some servers don't implement /models; try a minimal chat if credential present.
		if credential == "" && conn.CredentialRef != "none://" {
			return err
		}
		return a.pingChat(ctx, conn, credential)
	}
	_ = models
	return nil
}

func (a *Adapter) pingChat(ctx context.Context, conn config.ProviderConfig, credential string) error {
	// Lightweight: just GET base or models already failed — accept connectivity via OPTIONS/GET health.
	url := a.baseURL(conn) + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	a.applyAuth(req, conn, credential)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("authentication failed (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("provider unavailable (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (a *Adapter) ListModels(ctx context.Context, conn config.ProviderConfig, credential string) ([]provider.Model, error) {
	url := a.baseURL(conn) + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	a.applyAuth(req, conn, credential)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("list models HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	out := make([]provider.Model, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		out = append(out, provider.Model{ID: m.ID, DisplayName: m.ID})
	}
	return out, nil
}

func (a *Adapter) applyAuth(req *http.Request, conn config.ProviderConfig, credential string) {
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range conn.Headers {
		req.Header.Set(k, v)
	}
}

func (a *Adapter) Execute(ctx context.Context, nreq *normalization.NormalizedRequest, target provider.Target, credential string) (*normalization.NormalizedResponse, error) {
	payload := toOpenAIRequest(nreq, target.Model, false)
	raw, status, err := a.doJSON(ctx, target.Config, credential, "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, a.ClassifyError(status, raw, nil)
	}
	return fromOpenAIResponse(raw, nreq.RequestedModel, target)
}

func (a *Adapter) Stream(ctx context.Context, nreq *normalization.NormalizedRequest, target provider.Target, credential string) (provider.EventStream, error) {
	payload := toOpenAIRequest(nreq, target.Model, true)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := a.baseURL(target.Config) + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	a.applyAuth(req, target.Config, credential)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, a.ClassifyError(resp.StatusCode, b, nil)
	}
	return &openaiStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body), publicModel: nreq.RequestedModel}, nil
}

func (a *Adapter) doJSON(ctx context.Context, conn config.ProviderConfig, credential, path string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	url := a.baseURL(conn) + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	a.applyAuth(req, conn, credential)
	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	return raw, resp.StatusCode, err
}

func (a *Adapter) ClassifyError(status int, body []byte, err error) *normalization.Error {
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
			return &normalization.Error{Code: normalization.ErrUpstreamTimeout, Message: msg, HTTPStatus: 504, Retryable: true}
		}
		return &normalization.Error{Code: normalization.ErrProviderUnavailable, Message: msg, HTTPStatus: 503, Retryable: true}
	}
	msg := truncate(string(body), 300)
	switch status {
	case 401, 403:
		return &normalization.Error{Code: normalization.ErrProviderAuth, Message: "upstream authentication failed", HTTPStatus: 502, Retryable: false}
	case 404:
		return &normalization.Error{Code: normalization.ErrModelNotFound, Message: msg, HTTPStatus: 404, Retryable: false}
	case 429:
		return &normalization.Error{Code: normalization.ErrRateLimited, Message: msg, HTTPStatus: 429, Retryable: true}
	case 408, 500, 502, 503, 504:
		return &normalization.Error{Code: normalization.ErrProviderUnavailable, Message: msg, HTTPStatus: 503, Retryable: true}
	case 400:
		return &normalization.Error{Code: normalization.ErrInvalidRequest, Message: msg, HTTPStatus: 400, Retryable: false}
	default:
		if status >= 500 {
			return &normalization.Error{Code: normalization.ErrProviderUnavailable, Message: msg, HTTPStatus: 503, Retryable: true}
		}
		return &normalization.Error{Code: normalization.ErrInternal, Message: msg, HTTPStatus: 500, Retryable: false}
	}
}

// --- request/response mapping ---

func toOpenAIRequest(nreq *normalization.NormalizedRequest, model string, stream bool) map[string]any {
	msgs := make([]map[string]any, 0, len(nreq.Messages)+1)
	if nreq.System != "" {
		// Prefer system in messages for broad compatibility.
		hasSystem := false
		for _, m := range nreq.Messages {
			if m.Role == normalization.RoleSystem {
				hasSystem = true
				break
			}
		}
		if !hasSystem {
			msgs = append(msgs, map[string]any{"role": "system", "content": nreq.System})
		}
	}
	for _, m := range nreq.Messages {
		msgs = append(msgs, mapMessageToOpenAI(m))
	}
	out := map[string]any{
		"model":    model,
		"messages": msgs,
		"stream":   stream,
	}
	if nreq.Temperature != nil {
		out["temperature"] = *nreq.Temperature
	}
	if nreq.TopP != nil {
		out["top_p"] = *nreq.TopP
	}
	if nreq.MaxOutputTokens != nil {
		out["max_tokens"] = *nreq.MaxOutputTokens
	}
	if len(nreq.StopSequences) > 0 {
		out["stop"] = nreq.StopSequences
	}
	if nreq.ResponseFormat != nil {
		out["response_format"] = nreq.ResponseFormat
	}
	if len(nreq.Tools) > 0 {
		tools := make([]map[string]any, 0, len(nreq.Tools))
		for _, t := range nreq.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.InputSchema,
				},
			})
		}
		out["tools"] = tools
	}
	if nreq.ToolChoice != nil {
		switch nreq.ToolChoice.Type {
		case "auto", "none", "required":
			out["tool_choice"] = nreq.ToolChoice.Type
		case "tool":
			out["tool_choice"] = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": nreq.ToolChoice.Name},
			}
		}
	}
	if stream {
		out["stream_options"] = map[string]any{"include_usage": true}
	}
	return out
}

func mapMessageToOpenAI(m normalization.Message) map[string]any {
	msg := map[string]any{"role": string(m.Role)}
	// Tool results
	if m.Role == normalization.RoleTool {
		var text, callID string
		for _, c := range m.Content {
			if c.Type == normalization.ContentToolResult {
				text = c.Text
				callID = c.ToolCallID
			} else if c.Type == normalization.ContentText {
				text = c.Text
			}
		}
		msg["content"] = text
		if callID != "" {
			msg["tool_call_id"] = callID
		}
		return msg
	}
	// Assistant with tool calls
	var toolCalls []map[string]any
	var texts []string
	for _, c := range m.Content {
		switch c.Type {
		case normalization.ContentText:
			texts = append(texts, c.Text)
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
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
		if len(texts) > 0 {
			msg["content"] = strings.Join(texts, "")
		} else {
			msg["content"] = nil
		}
		return msg
	}
	if len(texts) == 1 {
		msg["content"] = texts[0]
	} else if len(texts) > 1 {
		parts := make([]map[string]any, 0, len(texts))
		for _, t := range texts {
			parts = append(parts, map[string]any{"type": "text", "text": t})
		}
		msg["content"] = parts
	} else {
		msg["content"] = ""
	}
	return msg
}

func fromOpenAIResponse(raw []byte, publicModel string, target provider.Target) (*normalization.NormalizedResponse, error) {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   any    `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("openai response has no choices")
	}
	ch := resp.Choices[0]
	var content []normalization.ContentBlock
	switch c := ch.Message.Content.(type) {
	case string:
		if c != "" {
			content = append(content, normalization.ContentBlock{Type: normalization.ContentText, Text: c})
		}
	case []any:
		for _, part := range c {
			if m, ok := part.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						content = append(content, normalization.ContentBlock{Type: normalization.ContentText, Text: t})
					}
				}
			}
		}
	}
	for _, tc := range ch.Message.ToolCalls {
		content = append(content, normalization.ContentBlock{
			Type:       normalization.ContentToolCall,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			Arguments:  tc.Function.Arguments,
		})
	}
	return &normalization.NormalizedResponse{
		ID:            resp.ID,
		Model:         publicModel,
		UpstreamModel: target.Model,
		ProviderID:    target.ProviderID,
		Content:       content,
		StopReason:    normalization.MapOpenAIStop(ch.FinishReason),
		Usage: normalization.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			Source:       "provider_reported",
		},
	}, nil
}

type openaiStream struct {
	body        io.ReadCloser
	scanner     *bufio.Scanner
	publicModel string
	started     bool
	toolIdx     map[int]string
	done        bool
}

func (s *openaiStream) Close() error {
	return s.body.Close()
}

func (s *openaiStream) Recv() (normalization.StreamEvent, error) {
	if s.done {
		return normalization.StreamEvent{}, io.EOF
	}
	if s.toolIdx == nil {
		s.toolIdx = map[int]string{}
	}
	if !s.started {
		s.started = true
		return normalization.StreamEvent{Type: normalization.EventMessageStart}, nil
	}
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			s.done = true
			return normalization.StreamEvent{Type: normalization.EventMessageStop, StopReason: normalization.StopEndTurn}, nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			return normalization.StreamEvent{
				Type: normalization.EventUsageDelta,
				Usage: &normalization.Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
					Source:       "provider_reported",
				},
			}, nil
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.Delta.Content != "" {
			return normalization.StreamEvent{
				Type:   normalization.EventTextDelta,
				Text:   ch.Delta.Content,
				Commit: true,
			}, nil
		}
		for _, tc := range ch.Delta.ToolCalls {
			if tc.ID != "" {
				s.toolIdx[tc.Index] = tc.ID
				return normalization.StreamEvent{
					Type:       normalization.EventToolCallStart,
					Index:      tc.Index,
					ToolCallID: tc.ID,
					ToolName:   tc.Function.Name,
					Commit:     true,
				}, nil
			}
			if tc.Function.Arguments != "" {
				id := s.toolIdx[tc.Index]
				return normalization.StreamEvent{
					Type:       normalization.EventToolCallDelta,
					Index:      tc.Index,
					ToolCallID: id,
					Arguments:  tc.Function.Arguments,
					Commit:     true,
				}, nil
			}
			if tc.Function.Name != "" {
				return normalization.StreamEvent{
					Type:     normalization.EventToolCallStart,
					Index:    tc.Index,
					ToolName: tc.Function.Name,
					Commit:   true,
				}, nil
			}
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			s.done = true
			return normalization.StreamEvent{
				Type:       normalization.EventMessageStop,
				StopReason: normalization.MapOpenAIStop(*ch.FinishReason),
			}, nil
		}
	}
	if err := s.scanner.Err(); err != nil {
		return normalization.StreamEvent{}, err
	}
	s.done = true
	return normalization.StreamEvent{Type: normalization.EventMessageStop, StopReason: normalization.StopEndTurn}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Ensure HTTP client respects context; used by tests to inject transport.
func (a *Adapter) SetHTTPClient(c *http.Client) {
	a.HTTPClient = c
}

// IdleTimeout helper for documentation; actual timeout is via context.
var _ = time.Second
