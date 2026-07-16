package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/provider"
)

const defaultURL = "https://api.anthropic.com"

// Adapter implements native Anthropic Messages API.
type Adapter struct {
	HTTPClient *http.Client
	DefaultURL string
}

func New() *Adapter {
	return &Adapter{
		HTTPClient: &http.Client{Timeout: 0},
		DefaultURL: defaultURL,
	}
}

func (a *Adapter) Type() string { return "anthropic" }

func (a *Adapter) Capabilities(conn config.ProviderConfig) provider.CapabilitySet {
	return provider.CapabilitySet{
		Chat: true, Streaming: true, Tools: true, SystemMessage: true, Vision: true,
	}
}

func (a *Adapter) baseURL(conn config.ProviderConfig) string {
	if conn.BaseURL != "" {
		return strings.TrimRight(conn.BaseURL, "/")
	}
	return strings.TrimRight(a.DefaultURL, "/")
}

func (a *Adapter) applyAuth(req *http.Request, conn config.ProviderConfig, credential string) {
	if credential != "" {
		req.Header.Set("x-api-key", credential)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	for k, v := range conn.Headers {
		req.Header.Set(k, v)
	}
}

func (a *Adapter) Validate(ctx context.Context, conn config.ProviderConfig, credential string) error {
	// Anthropic has no public list-models without billing; do a minimal invalid request to check auth.
	payload := map[string]any{
		"model":      "claude-3-haiku-20240307",
		"max_tokens": 1,
		"messages":   []map[string]any{{"role": "user", "content": "ping"}},
	}
	raw, status, err := a.doJSON(ctx, conn, credential, "/v1/messages", payload)
	if err != nil {
		return err
	}
	if status == 401 || status == 403 {
		return fmt.Errorf("authentication failed (HTTP %d)", status)
	}
	if status == 404 {
		// model not found but auth ok
		return nil
	}
	if status >= 500 {
		return fmt.Errorf("provider unavailable (HTTP %d): %s", status, truncate(string(raw), 200))
	}
	// 200 or 400 both mean we reached the API with valid auth often
	return nil
}

func (a *Adapter) ListModels(ctx context.Context, conn config.ProviderConfig, credential string) ([]provider.Model, error) {
	// Static common models; Anthropic model list API is limited.
	return []provider.Model{
		{ID: "claude-sonnet-4-20250514", DisplayName: "Claude Sonnet 4"},
		{ID: "claude-3-5-haiku-20241022", DisplayName: "Claude 3.5 Haiku"},
		{ID: "claude-3-haiku-20240307", DisplayName: "Claude 3 Haiku"},
	}, nil
}

func (a *Adapter) Execute(ctx context.Context, nreq *normalization.NormalizedRequest, target provider.Target, credential string) (*normalization.NormalizedResponse, error) {
	payload := toAnthropicRequest(nreq, target.Model, false)
	raw, status, err := a.doJSON(ctx, target.Config, credential, "/v1/messages", payload)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, a.ClassifyError(status, raw, nil)
	}
	return fromAnthropicResponse(raw, nreq.RequestedModel, target)
}

func (a *Adapter) Stream(ctx context.Context, nreq *normalization.NormalizedRequest, target provider.Target, credential string) (provider.EventStream, error) {
	payload := toAnthropicRequest(nreq, target.Model, true)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := a.baseURL(target.Config) + "/v1/messages"
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
	return &anthropicStream{body: resp.Body, scanner: bufio.NewScanner(resp.Body)}, nil
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

func toAnthropicRequest(nreq *normalization.NormalizedRequest, model string, stream bool) map[string]any {
	system := nreq.System
	msgs := make([]map[string]any, 0, len(nreq.Messages))
	for _, m := range nreq.Messages {
		if m.Role == normalization.RoleSystem {
			system += normalization.TextFromContent(m.Content)
			continue
		}
		msgs = append(msgs, mapAnthropicMessage(m))
	}
	maxTok := 4096
	if nreq.MaxOutputTokens != nil {
		maxTok = *nreq.MaxOutputTokens
	}
	out := map[string]any{
		"model":      model,
		"messages":   msgs,
		"max_tokens": maxTok,
		"stream":     stream,
	}
	if system != "" {
		out["system"] = system
	}
	if nreq.Temperature != nil {
		out["temperature"] = *nreq.Temperature
	}
	if nreq.TopP != nil {
		out["top_p"] = *nreq.TopP
	}
	if len(nreq.StopSequences) > 0 {
		out["stop_sequences"] = nreq.StopSequences
	}
	if len(nreq.Tools) > 0 {
		tools := make([]map[string]any, 0, len(nreq.Tools))
		for _, t := range nreq.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.InputSchema,
			})
		}
		out["tools"] = tools
	}
	if nreq.ToolChoice != nil {
		switch nreq.ToolChoice.Type {
		case "auto":
			out["tool_choice"] = map[string]any{"type": "auto"}
		case "none":
			out["tool_choice"] = map[string]any{"type": "none"}
		case "required":
			out["tool_choice"] = map[string]any{"type": "any"}
		case "tool":
			out["tool_choice"] = map[string]any{"type": "tool", "name": nreq.ToolChoice.Name}
		}
	}
	return out
}

func mapAnthropicMessage(m normalization.Message) map[string]any {
	role := string(m.Role)
	if m.Role == normalization.RoleTool {
		// Anthropic uses user role with tool_result blocks
		role = "user"
		blocks := make([]map[string]any, 0)
		for _, c := range m.Content {
			if c.Type == normalization.ContentToolResult {
				blocks = append(blocks, map[string]any{
					"type":        "tool_result",
					"tool_use_id": c.ToolCallID,
					"content":     c.Text,
					"is_error":    c.IsError,
				})
			}
		}
		return map[string]any{"role": role, "content": blocks}
	}
	var content any
	blocks := make([]map[string]any, 0, len(m.Content))
	for _, c := range m.Content {
		switch c.Type {
		case normalization.ContentText:
			blocks = append(blocks, map[string]any{"type": "text", "text": c.Text})
		case normalization.ContentToolCall:
			var input any
			_ = json.Unmarshal([]byte(c.Arguments), &input)
			if input == nil {
				input = map[string]any{}
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    c.ToolCallID,
				"name":  c.ToolName,
				"input": input,
			})
		}
	}
	if len(blocks) == 1 && blocks[0]["type"] == "text" {
		content = blocks[0]["text"]
	} else {
		content = blocks
	}
	return map[string]any{"role": role, "content": content}
}

func fromAnthropicResponse(raw []byte, publicModel string, target provider.Target) (*normalization.NormalizedResponse, error) {
	var resp struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	var content []normalization.ContentBlock
	for _, c := range resp.Content {
		switch c.Type {
		case "text":
			content = append(content, normalization.ContentBlock{Type: normalization.ContentText, Text: c.Text})
		case "tool_use":
			content = append(content, normalization.ContentBlock{
				Type:       normalization.ContentToolCall,
				ToolCallID: c.ID,
				ToolName:   c.Name,
				Arguments:  string(c.Input),
			})
		}
	}
	return &normalization.NormalizedResponse{
		ID:            resp.ID,
		Model:         publicModel,
		UpstreamModel: target.Model,
		ProviderID:    target.ProviderID,
		Content:       content,
		StopReason:    normalization.MapAnthropicStop(resp.StopReason),
		Usage: normalization.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
			Source:       "provider_reported",
		},
	}, nil
}

type anthropicStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	done    bool
}

func (s *anthropicStream) Close() error { return s.body.Close() }

func (s *anthropicStream) Recv() (normalization.StreamEvent, error) {
	if s.done {
		return normalization.StreamEvent{}, io.EOF
	}
	for s.scanner.Scan() {
		line := s.scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		var ev struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"content_block"`
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					InputTokens int `json:"input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			return normalization.StreamEvent{Type: normalization.EventMessageStart}, nil
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				return normalization.StreamEvent{
					Type:       normalization.EventToolCallStart,
					Index:      ev.Index,
					ToolCallID: ev.ContentBlock.ID,
					ToolName:   ev.ContentBlock.Name,
					Commit:     true,
				}, nil
			}
			return normalization.StreamEvent{Type: normalization.EventContentBlockStart, Index: ev.Index}, nil
		case "content_block_delta":
			if ev.Delta.Type == "text_delta" || ev.Delta.Text != "" {
				return normalization.StreamEvent{Type: normalization.EventTextDelta, Text: ev.Delta.Text, Commit: true}, nil
			}
			if ev.Delta.Type == "input_json_delta" || ev.Delta.PartialJSON != "" {
				return normalization.StreamEvent{
					Type:      normalization.EventToolCallDelta,
					Index:     ev.Index,
					Arguments: ev.Delta.PartialJSON,
					Commit:    true,
				}, nil
			}
		case "content_block_stop":
			return normalization.StreamEvent{Type: normalization.EventContentBlockStop, Index: ev.Index}, nil
		case "message_delta":
			if ev.Delta.StopReason != "" {
				return normalization.StreamEvent{
					Type:       normalization.EventMessageStop,
					StopReason: normalization.MapAnthropicStop(ev.Delta.StopReason),
					Usage:      &normalization.Usage{OutputTokens: ev.Usage.OutputTokens, Source: "provider_reported"},
				}, nil
			}
		case "message_stop":
			s.done = true
			return normalization.StreamEvent{Type: normalization.EventMessageStop, StopReason: normalization.StopEndTurn}, nil
		case "error":
			s.done = true
			return normalization.StreamEvent{
				Type: normalization.EventError,
				Error: &normalization.Error{
					Code: normalization.ErrProviderUnavailable, Message: "upstream stream error", HTTPStatus: 502,
				},
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
