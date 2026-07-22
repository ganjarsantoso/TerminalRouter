package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/termrouter/termrouter/internal/storage"
)

func int64Ptr(v int64) *int64 { return &v }

func TestPerKeyBodyLimit(t *testing.T) {
	key := &storage.ClientKey{ID: "k1", MaxRequestBody: int64Ptr(10)}
	withKey := func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), keyCtx{}, key))
	}

	cases := []struct {
		name       string
		contentLen int64
		body       string
		wantStatus int
	}{
		{"under limit", 5, "hello", http.StatusOK},
		{"at limit", 10, "0123456789", http.StatusOK},
		{"over limit by header", 11, "0123456789a", http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			h := PerKeyBodyLimit()(next)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
			req.ContentLength = tc.contentLen
			req = withKey(req)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestPerKeyBodyLimitSkipsHealth(t *testing.T) {
	key := &storage.ClientKey{ID: "k1", MaxRequestBody: int64Ptr(1)}
	h := PerKeyBodyLimit()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req = req.WithContext(context.WithValue(req.Context(), keyCtx{}, key))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health should bypass body limit, got %d", rec.Code)
	}
}

func TestPerKeyBodyLimitNoKeyUnlimited(t *testing.T) {
	h := PerKeyBodyLimit()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("x"))
	req.ContentLength = 1000000
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("no key = no per-key limit, got %d", rec.Code)
	}
}

// TestReservationPreventsConcurrentBypass ensures that in-flight requests are
// counted toward the daily request quota, so that N concurrent in-flight
// requests with a daily limit of N cannot collectively admit an (N+1)th (PRD §5).
func TestReservationPreventsConcurrentBypass(t *testing.T) {
	limit := 2
	key := &storage.ClientKey{ID: "k1", DailyRequestLimit: &limit}

	lim := NewLimiter(nil, nil)
	lim.PublicHosting = false

	// Simulate two in-flight requests via reservations.
	r1 := lim.reserve(key)
	r2 := lim.reserve(key)

	// A third admission attempt must be rejected because two are reserved.
	if err := lim.checkDaily(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil), key, time.Now().UTC()); err == nil {
		t.Fatalf("expected 429 with two reserved against limit 2")
	} else if err.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", err.HTTPStatus)
	}

	// Releasing one reservation lets a new admission through again.
	r1()
	if err := lim.checkDaily(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil), key, time.Now().UTC()); err != nil {
		t.Fatalf("after releasing one reservation, admission should pass, got %v", err)
	}

	// Releasing the second clears the reservation entirely.
	r2()
	if _, ok := lim.reserved[key.ID]; ok {
		t.Fatalf("reservation should be cleared after all releases")
	}
}
