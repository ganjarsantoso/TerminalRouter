package middleware

import (
	"context"
	"net/http"
	"strings"
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

// Auth validates router client keys (not upstream credentials).
type Auth struct {
	Store        *storage.Store
	AuthRequired bool
	Log          *observability.Logger
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
		keys, err := a.Store.FindEnabledKeys(r.Context())
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
			if !a.AuthRequired {
				next.ServeHTTP(w, r)
				return
			}
			a.securityEvent("invalid_key", r)
			writeAPIError(w, r, normalization.NewError(normalization.ErrAuthentication, "invalid client API key", 401))
			return
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
