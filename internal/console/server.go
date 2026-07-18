package console

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/termrouter/termrouter/internal/app"
	"github.com/termrouter/termrouter/internal/config"
	"github.com/termrouter/termrouter/internal/credentials"
	"github.com/termrouter/termrouter/internal/observability"
	"github.com/termrouter/termrouter/internal/storage"
)

// assetsFS holds embedded built frontend (populated via go:embed in assets.go).
var assetsFS embedFS = noEmbedFS{}

// Server is the optional local management Console for TermRouter.
type Server struct {
	App   *app.Server // optional, when gateway is co-located
	Store *storage.Store
	Creds *credentials.Manager
	Log   *observability.Logger
	Paths config.Paths
	Ctx   context.Context
	Home  string
	Port  int
	Host  string
	NoOpen bool

	// ReloadRuntime applies Console config changes to the live gateway.
	ReloadRuntime bool

	mu             sync.RWMutex
	http           *http.Server
	bootstrapToken string
	started        time.Time
	currentSession string
	reqCounter     atomic.Int64
	sessions       map[string]session
}

// Options configure Console startup.
type Options struct {
	Home   string
	Port   int
	Host   string
	NoOpen bool
	App    *app.Server
	Creds  *credentials.Manager
	Log    *observability.Logger
}

// New builds a Console server.
func New(opts Options) (*Server, error) {
	home := opts.Home
	if home == "" {
		d, err := config.DefaultRoot()
		if err != nil {
			return nil, err
		}
		home = d
	}
	paths := config.ResolvePaths(home)
	store, err := storage.Open(paths.Database)
	if err != nil {
		return nil, err
	}
	host := opts.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := opts.Port
	if port == 0 {
		port = 8788
	}
	log := opts.Log
	if log == nil {
		log, _ = observability.New("info", paths.LogsDir)
	}
	creds := opts.Creds
	if creds == nil {
		cfg, err := config.Load(paths.Config)
		if err == nil {
			creds, _ = credentials.NewManager(cfg.Credentials.Backend, paths.Vault, os.Getenv("TERMROUTER_VAULT_PASSPHRASE"))
		}
	}
	return &Server{
		App:           opts.App,
		Store:         store,
		Creds:         creds,
		Log:           log,
		Paths:         paths,
		Home:          home,
		Port:          port,
		Host:          host,
		NoOpen:        opts.NoOpen,
		Ctx:           context.Background(),
		ReloadRuntime: true,
		sessions:      map[string]session{},
		bootstrapToken: generateToken(),
	}, nil
}

// BootstrapURL returns the one-time login URL.
func (s *Server) BootstrapURL() string {
	s.mu.RLock()
	tok := s.bootstrapToken
	s.mu.RUnlock()
	return fmt.Sprintf("http://%s:%d/login?token=%s", s.Host, s.Port, tok)
}

func (s *Server) generateBootstrapToken() string {
	return generateToken()
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handler builds the Console HTTP handler (embedded assets + management API + auth).
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()

	// Auth endpoints (outside session requirement)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("POST /admin/v1/session/bootstrap", s.handleBootstrap)
	mux.HandleFunc("GET /admin/v1/csrf", s.handleCSRFToken)
	mux.HandleFunc("GET /admin/v1/session", s.requireSession(s.handleSession))
	mux.HandleFunc("DELETE /admin/v1/session", s.requireSession(s.handleLogout))
	mux.HandleFunc("POST /admin/v1/session/logout", s.requireSession(s.handleLogout))

	s.mountAPI(mux)

	// Embedded static assets (SPA)
	mux.Handle("/", s.requireSessionMaybe(s.staticHandler()))

	var h http.Handler = mux
	h = s.securityHeaders(h)
	return h
}

func (s *Server) staticHandler() http.Handler {
	fs := http.FileServer(http.FS(assetsFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA fallback: serve index.html for unknown non-asset routes
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := assetsFS.Open(p); err != nil {
			if !strings.Contains(p, ".") {
				r.URL.Path = "/"
			}
		}
		fs.ServeHTTP(w, r)
	})
}

// Start launches the Console. If the gateway is not already running it may be
// started separately by the CLI; Console always binds its own management port.
func (s *Server) Start(ctx context.Context) error {
	if !isLoopback(s.Host) {
		return fmt.Errorf("console must bind to loopback; non-loopback host %q rejected (security policy)", s.Host)
	}
	// Ensure a bootstrap token exists for first-login.
		s.mu.Lock()
		if s.bootstrapToken == "" {
			s.bootstrapToken = s.generateBootstrapToken()
		}
		if s.sessions == nil {
			s.sessions = map[string]session{}
		}
		s.mu.Unlock()

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("console listen %s: %w", addr, err)
	}

	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.started = time.Now()
	s.Ctx = ctx
	s.writePIDFile()

	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(ln) }()

	if !s.NoOpen {
		openBrowser(s.BootstrapURL())
	}

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

// Shutdown gracefully stops the Console.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http == nil {
		return nil
	}
	err := s.http.Shutdown(ctx)
	_ = s.Store.Close()
	return err
}

// writePIDFile records the console pid.
func (s *Server) writePIDFile() {
	_ = os.MkdirAll(s.Paths.RunDir, 0o700)
	_ = os.WriteFile(filepath.Join(s.Paths.RunDir, "console.pid"), []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
}

func isLoopback(host string) bool {
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sha256Hex is a small helper.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (s *Server) nextReqID() string {
	n := s.reqCounter.Add(1)
	return fmt.Sprintf("admin_req_%d", n)
}
