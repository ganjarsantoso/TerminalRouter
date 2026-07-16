package anthropic

import "testing"

func TestParseMessagesRequest(t *testing.T) {
	body := []byte(`{
		"model": "coding",
		"max_tokens": 128,
		"system": "sys",
		"messages": [
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "hi"},
				{"type": "tool_use", "id": "toolu_1", "name": "fn", "input": {"a": 1}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}
			]}
		],
		"tools": [{"name": "fn", "input_schema": {"type": "object"}}]
	}`)
	req, err := ParseMessagesRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.System != "sys" || *req.MaxOutputTokens != 128 {
		t.Fatalf("%+v", req)
	}
	if len(req.Tools) != 1 {
		t.Fatal(req.Tools)
	}
	if len(req.Messages) < 2 {
		t.Fatalf("messages %d", len(req.Messages))
	}
}
