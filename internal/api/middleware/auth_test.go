package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/storage"
)

// fakeStore is a minimal Store stub for auth tests.
type fakeStore struct {
	keys  []storage.ClientKey
	getByPrefix func(prefix string) []storage.ClientKey
	all        []storage.ClientKey
}

func (f *fakeStore) FindEnabledKeys(ctx context.Context) ([]storage.ClientKey, error) {
	return f.all, nil
}

func (f *fakeStore) FindEnabledKeysByPrefix(ctx context.Context, prefix string) ([]storage.ClientKey, error) {
	if f.getByPrefix != nil {
		return f.getByPrefix(prefix), nil
	}
	return f.keys, nil
}

func TestPrefixLookupAuthenticatesMatchingPrefix(t *testing.T) {
	pt, prefix, hash, salt, err := credentials.GenerateClientKey()
	if err != nil {
		t.Fatal(err)
	}
	k := storage.ClientKey{ID: "k1", KeyPrefix: prefix, KeyHash: hash, Salt: salt, Enabled: true, AllowedAliases: []string{"*"}}
	store := &fakeStore{}
	store.getByPrefix = func(p string) []storage.ClientKey {
		if p == prefix {
			return []storage.ClientKey{k}
		}
		return nil
	}
	a := NewAuth(store, true, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+pt)
	ctx := context.WithValue(req.Context(), clientIPCtx{}, "127.0.0.1")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	var gotKey *storage.ClientKey
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = ClientKeyFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	a.Middleware(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotKey == nil || gotKey.ID != "k1" {
		t.Fatal("expected authenticated key k1")
	}
}

func TestLookupFallsBackToFullScanWhenNoPrefix(t *testing.T) {
	pt, prefix, hash, salt, err := credentials.GenerateClientKey()
	if err != nil {
		t.Fatal(err)
	}
	k := storage.ClientKey{ID: "k1", KeyPrefix: prefix, KeyHash: hash, Salt: salt, Enabled: true, AllowedAliases: []string{"*"}}
	var prefixCalled bool
	store := &fakeStore{}
	store.getByPrefix = func(p string) []storage.ClientKey { prefixCalled = true; return nil }
	store.all = []storage.ClientKey{k}
	a := NewAuth(store, true, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+pt)
	ctx := context.WithValue(req.Context(), clientIPCtx{}, "127.0.0.1")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	a.Middleware(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !prefixCalled {
		t.Fatal("expected prefix lookup to be attempted first")
	}
}
