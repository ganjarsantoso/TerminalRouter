package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/storage"
)

type keyCtx struct{}

// ClientKeyFrom returns the authenticated client key from context.
func ClientKeyFrom(ctx context.Context) *storage.ClientKey {
	if v, ok := ctx.Value(keyCtx{}).(*storage.ClientKey); ok {
		return v
	}
	return nil
}

// invalidAuthThrottle slows repeated failed authentications from a single
// source IP to blunt online brute-force enumeration of client keys. It is
// best-effort and resets on config reload.
type invalidAuthThrottle struct {
	mu          sync.Mutex
	failures    map[string]int
	lastCleanup time.Time
}

const (
	invalidAuthMaxFailures = 20
	invalidAuthWindow      = 10 * time.Minute
)

func (t *invalidAuthThrottle) record(ip string) (throttled bool) {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failures == nil {
		t.failures = make(map[string]int)
	}
	if time.Since(t.lastCleanup) > time.Minute {
		for k, v := range t.failures {
			if v <= 0 {
				delete(t.failures, k)
			}
		}
		t.lastCleanup = time.Now()
	}
	t.failures[ip]++
	return t.failures[ip] > invalidAuthMaxFailures
}

func (t *invalidAuthThrottle) success(ip string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failures != nil {
		delete(t.failures, ip)
	}
}

// KeyLookup is the subset of storage used by Auth, allowing tests to inject a
// stub and keeping Auth decoupled from the concrete *storage.Store.
type KeyLookup interface {
	FindEnabledKeys(ctx context.Context) ([]storage.ClientKey, error)
	FindEnabledKeysByPrefix(ctx context.Context, prefix string) ([]storage.ClientKey, error)
}

// Auth validates router client keys (not upstream credentials).
type Auth struct {
	Store        KeyLookup
	AuthRequired bool
	Log          *observability.Logger

	throttle *invalidAuthThrottle
}

// NewAuth constructs an Auth with brute-force throttling enabled.
func NewAuth(store KeyLookup, authRequired bool, log *observability.Logger) *Auth {
	return &Auth{
		Store:        store,
		AuthRequired: authRequired,
		Log:          log,
		throttle:     &invalidAuthThrottle{},
	}
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health endpoints skip auth (readiness may be restricted at the reverse proxy).
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		token := extractToken(r)
		if token == "" {
			if !a.AuthRequired {
				next.ServeHTTP(w, r)
				return
			}
			a.securityEvent("missing_key", r)
			writeAPIError(w, r, normalization.NewError(normalization.ErrAuthentication, "missing client API key; use Authorization: Bearer tr_live_…", 401))
			return
		}
		if a.Store == nil {
			if !a.AuthRequired {
				next.ServeHTTP(w, r)
				return
			}
			writeAPIError(w, r, normalization.NewError(normalization.ErrInternal, "auth store unavailable", 500))
			return
		}
		ip := ClientIPFrom(r.Context())
		keys, err := a.lookupCandidates(r, token)
		if err != nil {
			writeAPIError(w, r, normalization.NewError(normalization.ErrInternal, "auth lookup failed", 500))
			return
		}
		var matched *storage.ClientKey
		for i := range keys {
			if credentials.VerifyClientKey(token, keys[i].Salt, keys[i].KeyHash) {
				matched = &keys[i]
				break
			}
		}
		if matched == nil {
			if a.throttle != nil && a.throttle.record(ip) {
				time.Sleep(200 * time.Millisecond)
			}
			if !a.AuthRequired {
				next.ServeHTTP(w, r)
				return
			}
			a.securityEvent("invalid_key", r)
			writeAPIError(w, r, normalization.NewError(normalization.ErrAuthentication, "invalid client API key", 401))
			return
		}
		if a.throttle != nil {
			a.throttle.success(ip)
		}
		if matched.Expired(time.Now().UTC()) {
			a.securityEvent("key_expired", r)
			writeAPIError(w, r, normalization.NewError(normalization.ErrAuthentication, "client key has expired", 401))
			return
		}
		ctx := context.WithValue(r.Context(), keyCtx{}, matched)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// lookupCandidates narrows verification to keys sharing the presented token's
// non-secret prefix, avoiding an Argon2 verify against every enabled key.
// The prefix is never sufficient to authenticate; full hash verification still
// runs on the small candidate set. Malformed tokens, or a prefix that matches
// no stored key, fall back to a full scan so legacy/edge tokens still verify.
func (a *Auth) lookupCandidates(r *http.Request, token string) ([]storage.ClientKey, error) {
	if prefix := credentials.ClientKeyLookupPrefix(token); prefix != "" {
		keys, err := a.Store.FindEnabledKeysByPrefix(r.Context(), prefix)
		if err != nil {
			return nil, err
		}
		if len(keys) > 0 {
			return keys, nil
		}
	}
	return a.Store.FindEnabledKeys(r.Context())
}

func (a *Auth) securityEvent(kind string, r *http.Request) {
	if a == nil || a.Log == nil {
		return
	}
	a.Log.Warn("security_event",
		"event", kind,
		"path", r.URL.Path,
		"client_ip", ClientIPFrom(r.Context()),
		"request_id", observability.RequestIDFrom(r.Context()),
	)
}

func extractToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		const p = "Bearer "
		if strings.HasPrefix(h, p) {
			return strings.TrimSpace(h[len(p):])
		}
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[7:])
		}
	}
	if k := r.Header.Get("x-api-key"); k != "" {
		return strings.TrimSpace(k)
	}
	return ""
}
