package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/termrouter/termrouter/internal/api/anthropic"
	"github.com/termrouter/termrouter/internal/api/middleware"
	openaiapi "github.com/termrouter/termrouter/internal/api/openai"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/execution"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/provider"
	panthropic "github.com/termrouter/termrouter/internal/provider/anthropic"
	"github.com/termrouter/termrouter/internal/provider/compatible"
	"github.com/termrouter/termrouter/internal/router"
	"github.com/termrouter/termrouter/internal/smart"
	"github.com/termrouter/termrouter/internal/storage"
)

// Server is the TermRouter HTTP gateway.
type Server struct {
	Cfg      *config.Config
	Paths    config.Paths
	Store    *storage.Store
	Creds    *credentials.Manager
	Log      *observability.Logger
	Registry *provider.Registry
	http     *http.Server
	started  time.Time
	active   atomic.Int64
	mu       sync.RWMutex
	handler  http.Handler
}

// New builds a server from loaded config and open storage.
func New(cfg *config.Config, paths config.Paths, store *storage.Store, creds *credentials.Manager, log *observability.Logger) *Server {
	reg := provider.NewRegistry()
	reg.Register(compatible.NewOpenAI())
	reg.Register(compatible.NewCompatible())
	reg.Register(panthropic.New())

	s := &Server{
		Cfg:      cfg,
		Paths:    paths,
		Store:    store,
		Creds:    creds,
		Log:      log,
		Registry: reg,
	}
	s.rebuildHandler()
	return s
}

// Reload updates the server's configuration and rebuilds the handler.
func (s *Server) Reload(cfg *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Cfg = cfg
	s.rebuildHandler()
	return nil
}

func (s *Server) rebuildHandler() {
	resolver := router.NewResolver(s.Cfg)
	coord := execution.New(s.Registry, s.Creds, s.Store, s.Log)
	coord.Cfg = s.Cfg
	timeout := s.Cfg.Server.RequestTimeout.Duration()
	if timeout == 0 {
		timeout = 180 * time.Second
	}
	credCheck := func(ref string) bool {
		if s.Creds == nil {
			return true
		}
		_, err := s.Creds.Resolve(ref)
		return err == nil
	}
	smartEng := smart.GatewayEngine(s.Cfg, s.Store, credCheck)

	oai := &openaiapi.Gateway{
		Resolver: resolver, Coordinator: coord, Store: s.Store, Log: s.Log,
		Cfg: s.Cfg, Smart: smartEng,
		AllowDirect: s.Cfg.Server.AllowDirectModel, RequestTimeout: timeout,
	}
	ant := &anthropic.Gateway{
		Resolver: resolver, Coordinator: coord, Store: s.Store, Log: s.Log,
		Cfg: s.Cfg, Smart: smartEng,
		AllowDirect: s.Cfg.Server.AllowDirectModel, RequestTimeout: timeout,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	// /ready is exposed publicly only when explicitly enabled. When public_hosting
	// is enabled and expose_ready is false, /ready returns 404 so it is never
	// reachable through the reverse proxy and cannot leak provider/storage details.
	if s.Cfg.PublicHosting.Enabled && !s.Cfg.PublicHosting.ExposeReady {
		mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		})
	} else {
		mux.HandleFunc("/ready", s.handleReady)
	}
	mux.HandleFunc("/v1/models", oai.ListModels)
	mux.HandleFunc("/v1/chat/completions", oai.ChatCompletions)
	mux.HandleFunc("/v1/messages", ant.Messages)

	maxSize, _ := config.ParseMaxRequestSize(s.Cfg.Server.MaxRequestSize)
	auth := middleware.NewAuth(s.Store, s.Cfg.Server.AuthRequired, s.Log)
	limiter := middleware.NewLimiter(s.Store, s.Log)
	limiter.PublicHosting = s.Cfg.PublicHosting.Enabled
	proxyNets, _ := middleware.ParseTrustedProxies(s.Cfg.Server.TrustedProxies)
	proxy := &middleware.TrustedProxy{Networks: proxyNets}
	globalConc := middleware.NewGlobalConcurrency(s.Cfg.Server.MaxConcurrency, s.Log)

	// Effective request order (outermost first). Authentication must run before
	// PerKeyBodyLimit so the per-key policy is known; global concurrency runs
	// after body limiting so oversized/unauthenticated requests are cheap to
	// reject; per-key limiter/quotas run last before the handler.
	s.handler = middleware.Chain(mux,
		middleware.RequestID,
		middleware.Recovery,
		proxy.Middleware,
		middleware.MaxBytes(maxSize),
		auth.Middleware,
		middleware.PerKeyBodyLimit(),
		globalConc.Middleware,
		limiter.Middleware,
		s.trackActive,
	)
}

// Handler returns the HTTP handler (useful for tests).
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		h := s.handler
		s.mu.RUnlock()
		h.ServeHTTP(w, r)
	})
}

func (s *Server) trackActive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.active.Add(1)
		defer s.active.Add(-1)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.Store == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","reason":"no_store"}`))
		return
	}
	if err := s.Store.DB().Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not_ready","reason":"db"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}

// Start listens and serves until ctx is cancelled or Shutdown is called.
func (s *Server) Start(ctx context.Context) error {
	if !isLoopback(s.Cfg.Server.Host) && !s.Cfg.Server.InsecureRemote {
		// Require explicit insecure for non-loopback without TLS (MVP has no TLS yet)
		return fmt.Errorf("binding to non-loopback host %q requires server.insecure_remote: true (TLS not yet configured)", s.Cfg.Server.Host)
	}

	addr := s.Cfg.Addr()
	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// IdleTimeout for keep-alive; request deadline is per-handler
		IdleTimeout: 120 * time.Second,
	}
	s.started = time.Now()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	if err := s.writePID(); err != nil && s.Log != nil {
		s.Log.Warn("failed to write pid file", "error", err.Error())
	}

	if s.Log != nil {
		s.Log.Info("termrouter listening", "addr", addr, "auth_required", s.Cfg.Server.AuthRequired)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.http.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// Shutdown gracefully drains.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := s.http.Shutdown(shutdownCtx)
	_ = os.Remove(s.Paths.PIDFile)
	return err
}

func (s *Server) writePID() error {
	_ = os.MkdirAll(s.Paths.RunDir, 0o700)
	return os.WriteFile(s.Paths.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

// Status returns runtime status for the CLI.
type Status struct {
	Address       string            `json:"address"`
	Uptime        string            `json:"uptime"`
	ActiveStreams int64             `json:"active_requests"`
	PID           int               `json:"pid,omitempty"`
	Running       bool              `json:"running"`
	Providers     map[string]string `json:"providers"`
	Aliases       []string          `json:"aliases"`
}

// RuntimeStatus builds a status snapshot (from live server or pid file).
func (s *Server) RuntimeStatus() Status {
	st := Status{
		Address:       s.Cfg.Addr(),
		ActiveStreams: s.active.Load(),
		Running:       s.http != nil && !s.started.IsZero(),
		Providers:     map[string]string{},
		Aliases:       router.NewResolver(s.Cfg).ListPublicModels(),
	}
	if !s.started.IsZero() {
		st.Uptime = time.Since(s.started).Round(time.Second).String()
		st.PID = os.Getpid()
	}
	for name, p := range s.Cfg.Providers {
		state := "enabled"
		if !p.IsEnabled() {
			state = "disabled"
		}
		st.Providers[name] = state
	}
	return st
}

func isLoopback(host string) bool {
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// LoadRuntime loads config, store, and credentials for CLI operations.
func LoadRuntime(root string) (*config.Config, config.Paths, *storage.Store, *credentials.Manager, error) {
	if root == "" {
		var err error
		root, err = config.DefaultRoot()
		if err != nil {
			return nil, config.Paths{}, nil, nil, err
		}
	}
	paths := config.ResolvePaths(root)
	cfg, err := config.Load(paths.Config)
	if err != nil {
		return nil, paths, nil, nil, err
	}
	store, err := storage.Open(paths.Database)
	if err != nil {
		return nil, paths, nil, nil, err
	}
	store.SetPricing(func(provider, model string, inTokens, outTokens int) (float64, bool) {
		return cfg.ComputeCost(provider, model, inTokens, outTokens)
	})
	creds, err := credentials.NewManager(cfg.Credentials.Backend, paths.Vault, os.Getenv("TERMROUTER_VAULT_PASSPHRASE"))
	if err != nil {
		_ = store.Close()
		return nil, paths, nil, nil, err
	}
	return cfg, paths, store, creds, nil
}
