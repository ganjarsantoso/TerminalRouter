package observability

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	canary := "tr_live_deadbeefcafebabe0123456789abcdef"
	out := Redact("Authorization: Bearer " + canary)
	if strings.Contains(out, "deadbeef") {
		t.Fatalf("leaked: %s", out)
	}
	out = Redact("sk-ant-api03-CANARYSECRETVALUE1234567890")
	if strings.Contains(out, "CANARY") {
		t.Fatalf("leaked sk: %s", out)
	}
	out = Redact("x-api-key: supersecretkeyvalue")
	if strings.Contains(out, "supersecret") {
		t.Fatalf("leaked x-api-key: %s", out)
	}
}
