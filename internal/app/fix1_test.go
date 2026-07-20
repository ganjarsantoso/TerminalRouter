package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/storage"
)

// fixHarness builds a full server handler backed by a mock upstream that counts
// how many times a provider is actually invoked. The returned modify hook lets a
// test customize the client key policy before it is inserted.
type fixHarness struct {
	handler      http.Handler
	key          string
	store        *storage.Store
	providerHits *atomic.Int64
	cleanup      func()
}

func newFixHarness(t *testing.T, modifyKey func(k *storage.ClientKey), modifyCfg func(c *config.Config)) *fixHarness {
	t.Helper()
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-mock",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 4},
		})
	}))
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
		Type: "openai-compatible", BaseURL: up.URL + "/v1", CredentialRef: "vault://mock",
	}
	cfg.Aliases["coding"] = config.AliasConfig{Provider: "mock", Model: "mock-model"}
	if modifyCfg != nil {
		modifyCfg(cfg)
	}
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
	k := storage.ClientKey{
		ID: "key_test", Name: "test", KeyPrefix: prefix, KeyHash: hash, Salt: salt,
		Enabled: true, CreatedAt: time.Now().UTC(),
	}
	if modifyKey != nil {
		modifyKey(&k)
	}
	if err := store.InsertClientKey(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	log, _ := observability.New("error", paths.LogsDir)
	srv := app.New(cfg, paths, store, creds, log)
	return &fixHarness{
		handler: srv.Handler(), key: pt, store: store, providerHits: &hits,
		cleanup: func() { _ = store.Close(); _ = log.Close() },
	}
}

func chatReq(t *testing.T, key, body string) *http.Request {
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req
}

func chatBody(model string, pad int) string {
	filler := strings.Repeat("x", pad)
	return `{"model":"` + model + `","messages":[{"role":"user","content":"` + filler + `"}]}`
}

// --- §3 Per-key body limit runs AFTER auth through the real chain ---

func TestPerKeyBodyLimitAfterAuth(t *testing.T) {
	limit := int64(200)
	h := newFixHarness(t, func(k *storage.ClientKey) { k.MaxRequestBody = &limit }, nil)
	defer h.cleanup()

	// Under limit -> handler executes (200).
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, chatReq(t, h.key, chatBody("coding", 10)))
	if rr.Code != 200 {
		t.Fatalf("under limit: expected 200 got %d %s", rr.Code, rr.Body.String())
	}

	// Over limit -> 413 and provider not called again.
	before := h.providerHits.Load()
	rr = httptest.NewRecorder()
	h.handler.ServeHTTP(rr, chatReq(t, h.key, chatBody("coding", 500)))
	if rr.Code != 413 {
		t.Fatalf("over limit: expected 413 got %d %s", rr.Code, rr.Body.String())
	}
	if h.providerHits.Load() != before {
		t.Fatalf("oversized request reached provider")
	}
}

func TestOversizedUnauthenticatedFailsAuthFirst(t *testing.T) {
	limit := int64(50)
	h := newFixHarness(t, func(k *storage.ClientKey) { k.MaxRequestBody = &limit }, nil)
	defer h.cleanup()

	before := h.providerHits.Load()
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, chatReq(t, "invalid-key", chatBody("coding", 5000)))
	if rr.Code != 401 {
		t.Fatalf("expected 401 (auth before provider) got %d %s", rr.Code, rr.Body.String())
	}
	if h.providerHits.Load() != before {
		t.Fatalf("oversized invalid-key request reached provider")
	}
}

func TestUnknownContentLengthCannotBypassLimit(t *testing.T) {
	limit := int64(120)
	h := newFixHarness(t, func(k *storage.ClientKey) { k.MaxRequestBody = &limit }, nil)
	defer h.cleanup()

	before := h.providerHits.Load()
	req := chatReq(t, h.key, chatBody("coding", 5000))
	req.ContentLength = -1 // simulate unknown/chunked length
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	if rr.Code != 413 {
		t.Fatalf("expected 413 for unknown-length oversized body got %d %s", rr.Code, rr.Body.String())
	}
	if h.providerHits.Load() != before {
		t.Fatalf("unknown-length oversized request reached provider")
	}
}

// --- §6 Direct-model authorization ---

func TestDirectModelAuthorization(t *testing.T) {
	restrict := func(k *storage.ClientKey) { k.AllowedAliases = []string{"coding"} }
	allowDirect := func(c *config.Config) { c.Server.AllowDirectModel = true }

	cases := []struct {
		name       string
		model      string
		modKey     func(k *storage.ClientKey)
		wantStatus int
	}{
		{"allowed alias", "coding", restrict, 200},
		{"unlisted alias", "other", restrict, 403},
		{"direct slash denied by default", "mock/mock-model", restrict, 403},
		{"direct colon denied by default", "mock:mock-model", restrict, 403},
		{"direct enabled listed", "mock/mock-model", func(k *storage.ClientKey) {
			k.AllowedAliases = []string{"coding"}
			k.AllowDirectModels = true
			k.AllowedDirectModels = []string{"mock/mock-model"}
		}, 200},
		{"direct enabled unlisted", "mock/other", func(k *storage.ClientKey) {
			k.AllowedAliases = []string{"coding"}
			k.AllowDirectModels = true
			k.AllowedDirectModels = []string{"mock/mock-model"}
		}, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newFixHarness(t, tc.modKey, allowDirect)
			defer h.cleanup()
			rr := httptest.NewRecorder()
			h.handler.ServeHTTP(rr, chatReq(t, h.key, chatBody(tc.model, 5)))
			if rr.Code != tc.wantStatus {
				t.Fatalf("%s: model %q expected %d got %d %s", tc.name, tc.model, tc.wantStatus, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestPortableKeyDefaultsNoDirectModels(t *testing.T) {
	h := newFixHarness(t, func(k *storage.ClientKey) {
		k.Portable = true
	}, func(c *config.Config) { c.Server.AllowDirectModel = true })
	defer h.cleanup()
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, chatReq(t, h.key, chatBody("mock/mock-model", 5)))
	if rr.Code != 403 {
		t.Fatalf("portable key should not use direct models by default, got %d %s", rr.Code, rr.Body.String())
	}
}

// --- §4 Quota fail-closed for portable keys ---

func TestQuotaFailClosedPortableKey(t *testing.T) {
	limitReq := 100
	h := newFixHarness(t, func(k *storage.ClientKey) {
		k.Portable = true
		k.DailyRequestLimit = &limitReq // forces usage lookup
	}, nil)
	defer h.cleanup()

	// Break only the usage store (not auth) by dropping request_log so
	// UsageForKeySince fails while client-key lookup still works.
	if _, err := h.store.DB().Exec(`DROP TABLE request_log`); err != nil {
		t.Fatal(err)
	}

	before := h.providerHits.Load()
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, chatReq(t, h.key, chatBody("coding", 5)))
	if rr.Code != 503 {
		t.Fatalf("portable key + store failure: expected 503 got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "quota_policy_unavailable") {
		t.Fatalf("expected quota_policy_unavailable code, body %s", rr.Body.String())
	}
	if h.providerHits.Load() != before {
		t.Fatalf("request reached provider despite unreadable quota state")
	}
}

// --- §9 Global concurrency across multiple keys ---

func TestGlobalConcurrencyLimit(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	started := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		started++
		mu.Unlock()
		<-release // block until the test releases
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "x",
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		})
	}))
	defer up.Close()

	root := t.TempDir()
	paths := config.ResolvePaths(root)
	_ = config.EnsureDirs(paths)
	cfg := config.Default()
	cfg.Server.AuthRequired = true
	cfg.Server.MaxConcurrency = 1
	cfg.Credentials.Backend = "vault"
	cfg.Providers["mock"] = config.ProviderConfig{Type: "openai-compatible", BaseURL: up.URL + "/v1", CredentialRef: "none://"}
	cfg.Aliases["coding"] = config.AliasConfig{Provider: "mock", Model: "m"}
	_ = config.Save(paths.Config, cfg)
	store, _ := storage.Open(paths.Database)
	defer store.Close()
	creds, _ := credentials.NewManager("vault", paths.Vault, "t")

	makeKey := func(id string) string {
		pt, prefix, hash, salt, _ := credentials.GenerateClientKey()
		_ = store.InsertClientKey(context.Background(), storage.ClientKey{
			ID: id, Name: id, KeyPrefix: prefix, KeyHash: hash, Salt: salt, Enabled: true, CreatedAt: time.Now().UTC(),
		})
		return pt
	}
	k1 := makeKey("k1")
	k2 := makeKey("k2")
	log, _ := observability.New("error", "")
	defer log.Close()
	h := app.New(cfg, paths, store, creds, log).Handler()

	// First request occupies the only slot.
	rr1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		h.ServeHTTP(rr1, chatReq(t, k1, chatBody("coding", 1)))
		close(done1)
	}()

	// Wait until the first request is in the upstream (slot held).
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		s := started
		mu.Unlock()
		if s >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("first request never reached upstream")
		case <-time.After(5 * time.Millisecond):
		}
	}

	// Second request (different key) should be rejected 503 immediately.
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, chatReq(t, k2, chatBody("coding", 1)))
	if rr2.Code != 503 {
		t.Fatalf("expected 503 global concurrency for second key, got %d %s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "server_concurrency_limit") {
		t.Fatalf("expected server_concurrency_limit, body %s", rr2.Body.String())
	}

	close(release)
	<-done1
	if rr1.Code != 200 {
		t.Fatalf("first request should succeed, got %d %s", rr1.Code, rr1.Body.String())
	}

	// After release, slot is free: a new request succeeds.
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, chatReq(t, k1, chatBody("coding", 1)))
	if rr3.Code != 200 {
		t.Fatalf("after release expected 200 got %d %s", rr3.Code, rr3.Body.String())
	}
}

// --- §10 Request-ID validation through the chain ---

func TestRequestIDValidation(t *testing.T) {
	h := newFixHarness(t, nil, nil)
	defer h.cleanup()

	cases := []struct {
		name     string
		id       string
		preserve bool
	}{
		{"valid", "abc-123_ABC.9", true},
		{"overlong", strings.Repeat("a", 100), false},
		{"control chars", "bad\nid", false},
		{"whitespace only", "   ", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := chatReq(t, h.key, chatBody("coding", 1))
			req.Header.Set("X-Request-ID", tc.id)
			rr := httptest.NewRecorder()
			h.handler.ServeHTTP(rr, req)
			got := rr.Header().Get("X-Request-ID")
			if got == "" {
				t.Fatal("no request id returned")
			}
			if tc.preserve && got != tc.id {
				t.Fatalf("expected preserved id %q got %q", tc.id, got)
			}
			if !tc.preserve && got == tc.id {
				t.Fatalf("expected replaced id, got original %q", got)
			}
		})
	}

	// Missing request ID is generated.
	req := chatReq(t, h.key, chatBody("coding", 1))
	rr := httptest.NewRecorder()
	h.handler.ServeHTTP(rr, req)
	if rr.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id was not generated")
	}
}
