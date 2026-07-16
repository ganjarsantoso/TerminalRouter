package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/storage"
)

func setupGateway(t *testing.T, upstream http.Handler) (handler http.Handler, clientKey string, cleanup func()) {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)

	root := t.TempDir()
	paths := config.ResolvePaths(root)
	if err := config.EnsureDirs(paths); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Server.AuthRequired = true
	cfg.Credentials.Backend = "vault"
	cfg.Providers["mock"] = config.ProviderConfig{
		Type:          "openai-compatible",
		BaseURL:       up.URL + "/v1",
		CredentialRef: "vault://mock",
	}
	cfg.Aliases["coding"] = config.AliasConfig{Provider: "mock", Model: "mock-model"}
	if err := config.Save(paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.NewManager("vault", paths.Vault, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creds.Store("mock", "sk-canary-UPSTREAM-SECRET"); err != nil {
		t.Fatal(err)
	}
	pt, prefix, hash, salt, err := credentials.GenerateClientKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertClientKey(context.Background(), storage.ClientKey{
		ID: "key_test", Name: "test", KeyPrefix: prefix, KeyHash: hash, Salt: salt,
		Enabled: true, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	log, _ := observability.New("error", paths.LogsDir)
	srv := app.New(cfg, paths, store, creds, log)
	return srv.Handler(), pt, func() {
		_ = store.Close()
		_ = log.Close()
	}
}

func TestHealthAndReady(t *testing.T) {
	h, key, cleanup := setupGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer cleanup()
	_ = key

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/health", nil))
	if rr.Code != 200 {
		t.Fatalf("health %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/ready", nil))
	if rr.Code != 200 {
		t.Fatalf("ready %d %s", rr.Code, rr.Body.String())
	}
}

func TestChatCompletionsAuthRequired(t *testing.T) {
	h, _, cleanup := setupGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer cleanup()

	body := `{"model":"coding","messages":[{"role":"user","content":"hi"}]}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("expected 401 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestChatCompletionsNonStreaming(t *testing.T) {
	h, key, cleanup := setupGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		// ensure upstream auth header present but we don't echo secrets
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "no auth", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-1",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "hello from mock",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 4},
		})
	}))
	defer cleanup()

	body := `{"model":"coding","messages":[{"role":"user","content":"hi"}],"max_tokens":16}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["model"] != "coding" {
		t.Fatalf("public model should be alias, got %v", resp["model"])
	}
	choices := resp["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hello from mock" {
		t.Fatalf("content %v", msg["content"])
	}
}

func TestChatCompletionsStreaming(t *testing.T) {
	h, key, cleanup := setupGateway(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		chunks := []string{
			`{"id":"c1","choices":[{"delta":{"content":"hel"},"finish_reason":null}]}`,
			`{"id":"c1","choices":[{"delta":{"content":"lo"},"finish_reason":null}]}`,
			`{"id":"c1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, c := range chunks {
			_, _ = io.WriteString(w, "data: "+c+"\n\n")
			fl.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer cleanup()

	body := `{"model":"coding","messages":[{"role":"user","content":"hi"}],"stream":true}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	out := rr.Body.String()
	if !strings.Contains(out, "hel") || !strings.Contains(out, "[DONE]") {
		t.Fatalf("stream body: %s", out)
	}
}

func TestFallbackBeforeCommit(t *testing.T) {
	// first target 429, second succeeds
	var hits int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "ok",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "from-second"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer up.Close()

	root := t.TempDir()
	paths := config.ResolvePaths(root)
	_ = config.EnsureDirs(paths)
	cfg := config.Default()
	cfg.Credentials.Backend = "vault"
	// two providers pointing at same mock server (hits track attempts)
	cfg.Providers["p1"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: up.URL + "/v1", CredentialRef: "none://"}
	cfg.Providers["p2"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: up.URL + "/v1", CredentialRef: "none://"}
	cfg.Routes["r"] = config.RouteConfig{
		Strategy: "fallback",
		Targets: []config.TargetConfig{
			{Provider: "p1", Model: "m1"},
			{Provider: "p2", Model: "m2"},
		},
	}
	cfg.Aliases["coding"] = config.AliasConfig{Route: "r"}
	_ = config.Save(paths.Config, cfg)
	store, _ := storage.Open(paths.Database)
	defer store.Close()
	creds, _ := credentials.NewManager("vault", paths.Vault, "t")
	pt, prefix, hash, salt, _ := credentials.GenerateClientKey()
	_ = store.InsertClientKey(context.Background(), storage.ClientKey{
		ID: "k", Name: "t", KeyPrefix: prefix, KeyHash: hash, Salt: salt, Enabled: true, CreatedAt: time.Now().UTC(),
	})
	log, _ := observability.New("error", "")
	srv := app.New(cfg, paths, store, creds, log)

	body := `{"model":"coding","messages":[{"role":"user","content":"hi"}]}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+pt)
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("from-second")) {
		t.Fatalf("body %s", rr.Body.String())
	}
	if hits < 2 {
		t.Fatalf("expected fallback hits>=2 got %d", hits)
	}
}

func TestModelsList(t *testing.T) {
	h, key, cleanup := setupGateway(t, http.NotFoundHandler())
	defer cleanup()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "coding") {
		t.Fatal(rr.Body.String())
	}
}

func TestConfigExportNoSecrets(t *testing.T) {
	// canary should not appear in export
	canary := "sk-canary-EXPORT-TEST-SECRET"
	root := t.TempDir()
	paths := config.ResolvePaths(root)
	_ = config.EnsureDirs(paths)
	cfg := config.Default()
	cfg.Credentials.Backend = "vault"
	cfg.Providers["x"] = config.ProviderConfig{Type: "openai", CredentialRef: "vault://x"}
	_ = config.Save(paths.Config, cfg)
	creds, _ := credentials.NewManager("vault", paths.Vault, "p")
	_, _ = creds.Store("x", canary)
	san := cfg.ExportSanitized()
	b, _ := json.Marshal(san)
	if strings.Contains(string(b), canary) {
		t.Fatal("secret in export")
	}
	// vault file should not be readable as plaintext
	raw, _ := os.ReadFile(paths.Vault)
	if bytes.Contains(raw, []byte(canary)) {
		t.Fatal("plaintext secret on disk")
	}
	_ = filepath.Separator
}
