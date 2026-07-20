package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
)

// requestIDRE bounds acceptable client-supplied request IDs: 1-64 chars of
// ASCII letters, digits, hyphen, underscore, and period. Anything else (control
// characters, whitespace, overlong, non-ASCII) is rejected and replaced with a
// server-generated ID so it can never be reflected into headers, logs, or SQLite.
var requestIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidRequestID reports whether a client-supplied request ID is safe to reuse.
func ValidRequestID(id string) bool {
	return requestIDRE.MatchString(id)
}

func generateRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RequestID injects a request id, validating any client-supplied X-Request-ID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !ValidRequestID(id) {
			id = generateRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := observability.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// MaxBytes limits request body size.
func MaxBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && n > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recovery catches panics from downstream handlers and returns a 500 instead
// of letting the connection hang or leak a stack trace.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeAPIError(w, r, normalization.NewError(normalization.ErrInternal, "internal server error", 500))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeAPIError(w http.ResponseWriter, r *http.Request, err *normalization.Error) {
	if err == nil {
		err = normalization.NewError(normalization.ErrInternal, "unknown error", 500)
	}
	// OpenAI-style vs Anthropic-style based on path
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)
	if strings.HasPrefix(r.URL.Path, "/v1/messages") {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "error",
			"error": map[string]any{
				"type":    err.Code,
				"message": err.Message,
			},
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": err.Message,
			"type":    err.Code,
			"code":    err.Code,
		},
	})
}

// WriteError is exported for handlers.
func WriteError(w http.ResponseWriter, r *http.Request, err *normalization.Error) {
	writeAPIError(w, r, err)
}
