package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/storage"
)

// Limiter enforces per-key request-rate, concurrency, and daily quotas.
type Limiter struct {
	Store *storage.Store
	Log   *observability.Logger

	mu      sync.Mutex
	windows map[string]*rpmWindow
	active  map[string]int
}

type rpmWindow struct {
	times []time.Time
}

// NewLimiter builds a zero-value limiter ready for use.
func NewLimiter(store *storage.Store, log *observability.Logger) *Limiter {
	return &Limiter{
		Store:   store,
		Log:     log,
		windows: map[string]*rpmWindow{},
		active:  map[string]int{},
	}
}

// Middleware checks key policy limits after authentication.
// It should run after Auth so ClientKeyFrom is populated.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip non-inference and health paths.
		path := r.URL.Path
		if path == "/health" || path == "/ready" || path == "/v1/models" {
			next.ServeHTTP(w, r)
			return
		}
		ck := ClientKeyFrom(r.Context())
		if ck == nil || l == nil {
			next.ServeHTTP(w, r)
			return
		}
		now := time.Now().UTC()
		if ck.Expired(now) {
			l.securityEvent("key_expired", ck.ID, ck.Name, r)
			writeAPIError(w, r, normalization.NewError(normalization.ErrAuthentication, "client key has expired", 401))
			return
		}

		if err := l.checkRPM(ck); err != nil {
			l.securityEvent("rate_limit_rpm", ck.ID, ck.Name, r)
			w.Header().Set("Retry-After", "60")
			writeAPIError(w, r, err)
			return
		}
		if err := l.acquireConcurrency(ck); err != nil {
			l.securityEvent("concurrency_limit", ck.ID, ck.Name, r)
			w.Header().Set("Retry-After", "5")
			writeAPIError(w, r, err)
			return
		}
		// Release concurrency when the handler finishes.
		defer l.releaseConcurrency(ck.ID)

		if err := l.checkDaily(r, ck, now); err != nil {
			l.securityEvent("daily_quota", ck.ID, ck.Name, r)
			// Retry after midnight UTC.
			midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
			w.Header().Set("Retry-After", strconv.Itoa(int(midnight.Sub(now).Seconds())))
			writeAPIError(w, r, err)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (l *Limiter) checkRPM(ck *storage.ClientKey) *normalization.Error {
	if ck.RateLimitRPM == nil || *ck.RateLimitRPM <= 0 {
		return nil
	}
	limit := *ck.RateLimitRPM
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.windows[ck.ID]
	if w == nil {
		w = &rpmWindow{}
		l.windows[ck.ID] = w
	}
	// Drop timestamps outside the window.
	kept := w.times[:0]
	for _, t := range w.times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	w.times = kept
	if len(w.times) >= limit {
		return normalization.NewError(normalization.ErrRateLimited,
			"requests-per-minute limit exceeded for this client key", 429)
	}
	w.times = append(w.times, now)
	return nil
}

func (l *Limiter) acquireConcurrency(ck *storage.ClientKey) *normalization.Error {
	if ck.MaxConcurrentRequests == nil || *ck.MaxConcurrentRequests <= 0 {
		return nil
	}
	limit := *ck.MaxConcurrentRequests
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[ck.ID] >= limit {
		return normalization.NewError(normalization.ErrRateLimited,
			"max concurrent requests exceeded for this client key", 429)
	}
	l.active[ck.ID]++
	return nil
}

func (l *Limiter) releaseConcurrency(keyID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[keyID] > 0 {
		l.active[keyID]--
	}
	if l.active[keyID] == 0 {
		delete(l.active, keyID)
	}
}

func (l *Limiter) checkDaily(r *http.Request, ck *storage.ClientKey, now time.Time) *normalization.Error {
	needsUsage := (ck.DailyRequestLimit != nil && *ck.DailyRequestLimit > 0) ||
		(ck.DailyInputTokens != nil && *ck.DailyInputTokens > 0) ||
		(ck.DailyOutputTokens != nil && *ck.DailyOutputTokens > 0) ||
		(ck.DailyEstimatedCostUSD != nil && *ck.DailyEstimatedCostUSD > 0)
	if !needsUsage || l.Store == nil {
		return nil
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	usage, err := l.Store.UsageForKeySince(r.Context(), ck.ID, dayStart)
	if err != nil {
		return nil // fail open on store errors for availability; limits still apply via RPM/concurrency
	}
	if ck.DailyRequestLimit != nil && *ck.DailyRequestLimit > 0 && usage.Requests >= *ck.DailyRequestLimit {
		return normalization.NewError(normalization.ErrRateLimited,
			"daily request quota exceeded for this client key", 429)
	}
	if ck.DailyInputTokens != nil && *ck.DailyInputTokens > 0 && usage.InputTokens >= *ck.DailyInputTokens {
		return normalization.NewError(normalization.ErrRateLimited,
			"daily input-token quota exceeded for this client key", 429)
	}
	if ck.DailyOutputTokens != nil && *ck.DailyOutputTokens > 0 && usage.OutputTokens >= *ck.DailyOutputTokens {
		return normalization.NewError(normalization.ErrRateLimited,
			"daily output-token quota exceeded for this client key", 429)
	}
	if ck.DailyEstimatedCostUSD != nil && *ck.DailyEstimatedCostUSD > 0 && usage.EstimatedUSD >= *ck.DailyEstimatedCostUSD {
		return normalization.NewError(normalization.ErrRateLimited,
			"daily estimated-spend budget exceeded for this client key", 429)
	}
	return nil
}

// PerKeyBodyLimit enforces the per-key request body size cap (PRD §9/§10).
// It runs after authentication so the key policy is known, and rejects
// oversized requests before the body is read (413). Content-Length is
// authoritative when present; when absent we still cap via MaxBytesReader.
func PerKeyBodyLimit() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/health" || path == "/ready" || path == "/v1/models" {
				next.ServeHTTP(w, r)
				return
			}
			ck := ClientKeyFrom(r.Context())
			if ck == nil || ck.MaxRequestBody == nil || *ck.MaxRequestBody <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			limit := *ck.MaxRequestBody
			if r.ContentLength > limit {
				writeAPIError(w, r, normalization.NewError(normalization.ErrRequestTooLarge,
					"request body exceeds the limit for this client key", 413))
				return
			}
			if r.Body != nil && limit > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *Limiter) securityEvent(kind, keyID, keyName string, r *http.Request) {
	if l == nil || l.Log == nil {
		return
	}
	l.Log.Warn("security_event",
		"event", kind,
		"key_id", keyID,
		"key_name", keyName,
		"path", r.URL.Path,
		"client_ip", ClientIPFrom(r.Context()),
		"request_id", observability.RequestIDFrom(r.Context()),
	)
}
