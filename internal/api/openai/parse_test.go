package openai

import (
	"testing"

	"github.com/termrouter/termrouter/internal/normalization"
)

func TestParseChatRequest(t *testing.T) {
	body := []byte(`{
		"model": "coding",
		"messages": [
			{"role": "system", "content": "be brief"},
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "lookup", "arguments": "{}"}}
			]},
			{"role": "tool", "tool_call_id": "call_1", "content": "result"}
		],
		"tools": [{"type": "function", "function": {"name": "lookup", "parameters": {"type": "object"}}}],
		"max_tokens": 100,
		"stream": true
	}`)
	req, err := ParseChatRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.RequestedModel != "coding" || !req.Stream {
		t.Fatalf("%+v", req)
	}
	if req.System != "be brief" {
		t.Fatalf("system %q", req.System)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "lookup" {
		t.Fatal(req.Tools)
	}
	// system extracted, 3 messages remain
	if len(req.Messages) != 3 {
		t.Fatalf("messages %d", len(req.Messages))
	}
	if req.Messages[1].Content[0].Type != normalization.ContentToolCall {
		t.Fatal(req.Messages[1])
	}
	if req.Messages[2].Role != normalization.RoleTool {
		t.Fatal(req.Messages[2])
	}
}
