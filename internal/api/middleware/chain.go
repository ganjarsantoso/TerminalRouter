package middleware

import (
	"net/http"
	"sync/atomic"

	"github.com/termrouter/termrouter/internal/normalization"
	"github.com/termrouter/termrouter/internal/observability"
)

// Middleware is a standard http.Handler wrapper.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware around a base handler in request order: the first
// middleware in the list is the OUTERMOST wrapper and therefore runs FIRST for
// an incoming request, while the base handler runs LAST. This avoids the
// error-prone reverse wrapping of manual h = mw(h) chains.
//
// Example (request order top-to-bottom):
//
//	Chain(mux,
//	    RequestID,        // runs first
//	    Recovery,
//	    proxy.Middleware,
//	    MaxBytes(n),
//	    auth.Middleware,
//	    PerKeyBodyLimit(),
//	    global.Middleware,
//	    limiter.Middleware,
//	    trackActive,      // runs last, closest to mux
//	)
func Chain(base http.Handler, mws ...Middleware) http.Handler {
	h := base
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// GlobalConcurrency enforces a process-wide cap on the number of in-flight
// inference requests, independent of any per-key limit. Health and readiness
// probes are exempt so they remain answerable under load. A value <= 0 disables
// the limit.
type GlobalConcurrency struct {
	sem chan struct{}
	Log *observability.Logger

	inFlight atomic.Int64
}

// NewGlobalConcurrency builds a global concurrency guard. limit <= 0 disables it.
func NewGlobalConcurrency(limit int, log *observability.Logger) *GlobalConcurrency {
	g := &GlobalConcurrency{Log: log}
	if limit > 0 {
		g.sem = make(chan struct{}, limit)
	}
	return g
}

// InFlight returns the current number of held slots (for tests/metrics).
func (g *GlobalConcurrency) InFlight() int64 { return g.inFlight.Load() }

// Middleware acquires a slot before the handler runs and releases it after the
// handler returns (including after streaming completes, error, or panic).
func (g *GlobalConcurrency) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g == nil || g.sem == nil {
			next.ServeHTTP(w, r)
			return
		}
		if p := r.URL.Path; p == "/health" || p == "/ready" {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case g.sem <- struct{}{}:
			g.inFlight.Add(1)
			defer func() {
				<-g.sem
				g.inFlight.Add(-1)
			}()
		default:
			if g.Log != nil {
				g.Log.Warn("security_event",
					"event", "server_concurrency_limit",
					"path", r.URL.Path,
					"client_ip", ClientIPFrom(r.Context()),
					"request_id", observability.RequestIDFrom(r.Context()),
				)
			}
			w.Header().Set("Retry-After", "2")
			writeAPIError(w, r, normalization.NewError(normalization.ErrServerConcurrency,
				"server is at capacity; please retry shortly", 503))
			return
		}
		next.ServeHTTP(w, r)
	})
}
