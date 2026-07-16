package credentials

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientKeyHashVerify(t *testing.T) {
	pt, prefix, hash, salt, err := GenerateClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pt, "tr_live_") {
		t.Fatalf("key %q", pt)
	}
	if !strings.HasPrefix(prefix, "tr_live_") {
		t.Fatalf("prefix %q", prefix)
	}
	if !VerifyClientKey(pt, salt, hash) {
		t.Fatal("verify failed")
	}
	if VerifyClientKey(pt+"x", salt, hash) {
		t.Fatal("should not verify")
	}
}

func TestVaultStoreResolve(t *testing.T) {
	dir := t.TempDir()
	vault := filepath.Join(dir, "vault.db")
	m, err := NewManager("vault", vault, "test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := m.Store("openai-main", "sk-canary-SECRET-do-not-leak")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "vault://openai-main" {
		t.Fatalf("ref %q", ref)
	}
	// reload
	m2, err := NewManager("vault", vault, "test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := m2.Resolve(ref)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "sk-canary-SECRET-do-not-leak" {
		t.Fatal("secret mismatch")
	}
}

func TestEnvResolve(t *testing.T) {
	m, err := NewManager("env", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv("TERMROUTER_TEST_ENV_KEY", "value123")
	v, err := m.Resolve("env://TERMROUTER_TEST_ENV_KEY")
	if err != nil || v != "value123" {
		t.Fatalf("%q %v", v, err)
	}
}

func TestRedactSecret(t *testing.T) {
	s := RedactSecret("tr_live_abcdef0123456789")
	if strings.Contains(s, "abcdef") {
		t.Fatalf("not redacted: %s", s)
	}
}
