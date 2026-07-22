package optimization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/termrouter/termrouter/internal/config"
)

// ProtocolVersion is the wire protocol spoken by generic compressor adapters.
const CompressorProtocol = "termrouter-compressor/1"

// ErrCompressorResponseTooLarge is returned when a compressor response exceeds
// the configured MaxResponseBytes limit.
var ErrCompressorResponseTooLarge = errors.New("compressor response too large")

// ProtectedSpan marks a byte range that must not be altered by a compressor.
type ProtectedSpan struct {
	Start  int
	End    int
	Reason string
}

// CompressionRequest carries only approved, non-secret content to a plug-in.
type CompressionRequest struct {
	RequestID       string          `json:"request_id"`
	ContentClass    string          `json:"content_class"`
	Text            string          `json:"text"`
	TargetTokens    int             `json:"target_tokens"`
	ProtectedSpans  []ProtectedSpan `json:"protected_spans,omitempty"`
	PreservePattern []string        `json:"preserve_pattern,omitempty"`
	Language        string          `json:"language,omitempty"`
	ModelFamily     string          `json:"model_family,omitempty"`
}

// CompressionResponse is the validated response from a compressor plug-in.
type CompressionResponse struct {
	Protocol     string   `json:"protocol"`
	Text         string   `json:"text"`
	InputTokens  int      `json:"input_tokens"`
	OutputTokens int      `json:"output_tokens"`
	LossClass    string   `json:"loss_class"`
	Warnings     []string `json:"warnings,omitempty"`
	Model        string   `json:"model"`
	Version      string   `json:"version"`
}

// Compressor is an optional external semantic-compression plug-in.
type Compressor interface {
	Name() string
	Version(ctx context.Context) (string, error)
	Health(ctx context.Context) error
	Compress(ctx context.Context, req CompressionRequest) (*CompressionResponse, error)
}

// circuitBreaker prevents hammering an unavailable compressor.
type circuitBreaker struct {
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	threshold int
	cooldown  time.Duration
}

func newCircuitBreaker() *circuitBreaker {
	return &circuitBreaker{threshold: 5, cooldown: 30 * time.Second}
}

func (c *circuitBreaker) allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.openUntil) {
		return false
	}
	return true
}

func (c *circuitBreaker) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.openUntil = time.Time{}
}

func (c *circuitBreaker) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= c.threshold {
		c.openUntil = time.Now().Add(c.cooldown)
	}
}

// httpCompressor speaks the termrouter-compressor/1 protocol over HTTP or a
// Unix domain socket. It validates that HTTP endpoints are loopback unless
// explicitly permitted, never attaches credentials, and enforces a timeout.
type httpCompressor struct {
	name       string
	cfg        config.CompressorConfig
	client     *http.Client
	breaker    *circuitBreaker
	allowNonLB bool
}

func newHTTPCompressor(name string, cfg config.CompressorConfig) *httpCompressor {
	var dialCtx func(context.Context, string, string) (net.Conn, error)
	if cfg.AllowNonLoopback {
		dialCtx = func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return d.DialContext(ctx, network, addr)
		}
	} else {
		dialCtx = validatedDialContext
	}
	transport := &http.Transport{
		// Enforce connect, TLS, response-header, and idle timeouts.
		DialContext:           dialCtx,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if cfg.Transport == "unix" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", cfg.Endpoint)
		}
	}
	timeout := cfg.Timeout.Duration()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// Disable redirects by default; every redirect is re-evaluated.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &httpCompressor{
		name:       name,
		cfg:        cfg,
		client:     client,
		breaker:    newCircuitBreaker(),
		allowNonLB: cfg.AllowNonLoopback,
	}
}

// validatedDialContext resolves the host and applies dial protections.
func validatedDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	// Reject non-loopback destinations unless explicitly permitted.
	if ips, err := net.LookupIP(host); err == nil {
		for _, ip := range ips {
			if !ip.IsLoopback() {
				return nil, fmt.Errorf("compressor dial rejected: %s resolves to non-loopback %s", host, ip)
			}
		}
	}
	d := net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return d.DialContext(ctx, network, addr)
}

func (h *httpCompressor) Name() string { return h.name }

func (h *httpCompressor) Version(ctx context.Context) (string, error) {
	return h.call(ctx, CompressionRequest{ContentClass: "health"})
}

func (h *httpCompressor) Health(ctx context.Context) error {
	if !h.breaker.allow() {
		return fmt.Errorf("compressor %q circuit open", h.name)
	}
	_, err := h.call(ctx, CompressionRequest{ContentClass: "health"})
	if err != nil {
		h.breaker.recordFailure()
		return err
	}
	h.breaker.recordSuccess()
	return nil
}

func (h *httpCompressor) Compress(ctx context.Context, req CompressionRequest) (*CompressionResponse, error) {
	if !h.breaker.allow() {
		return nil, fmt.Errorf("compressor %q circuit open", h.name)
	}
	raw, err := h.call(ctx, req)
	if err != nil {
		h.breaker.recordFailure()
		return nil, err
	}
	h.breaker.recordSuccess()
	resp, err := validateCompressorResponse([]byte(raw))
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// call posts the request and returns the raw response body string.
func (h *httpCompressor) call(ctx context.Context, req CompressionRequest) (string, error) {
	if h.cfg.Transport == "http" && !h.allowNonLB {
		if err := assertLoopback(h.cfg.Endpoint); err != nil {
			return "", err
		}
	}
	// Reject URL userinfo (never attach credentials to compressor requests).
	if u, err := url.Parse(h.postURL()); err == nil {
		if u.User != nil {
			return "", fmt.Errorf("compressor %q: URL userinfo is forbidden", h.name)
		}
	}
	if req.RequestID == "" {
		req.RequestID = "optimization"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	// Reject request body larger than configured limit before network transmission.
	maxReqBytes := h.cfg.MaxRequestBytes
	if maxReqBytes <= 0 {
		maxReqBytes = 8 << 20
	}
	if len(body) > maxReqBytes {
		return "", fmt.Errorf("compressor %q: request body %d bytes exceeds max_request_bytes %d", h.name, len(body), maxReqBytes)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.postURL(), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	// Deliberately no Authorization header: compressors never receive credentials.
	resp, err := h.client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("compressor %q returned status %d", h.name, resp.StatusCode)
	}
	// Strict response-body size enforcement: read one byte past the limit so we
	// can distinguish "exactly at limit" from "exceeded limit".
	maxRespBytes := h.cfg.MaxResponseBytes
	if maxRespBytes <= 0 {
		maxRespBytes = 8 << 20
	}
	reader := io.LimitReader(resp.Body, int64(maxRespBytes)+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if len(data) > maxRespBytes {
		return "", ErrCompressorResponseTooLarge
	}
	return string(data), nil
}

func (h *httpCompressor) postURL() string {
	if h.cfg.Transport == "unix" {
		return "http://unix/" // host irrelevant for unix transport
	}
	return h.cfg.Endpoint
}

// assertLoopback ensures an http(s) endpoint resolves to loopback.
func assertLoopback(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid compressor endpoint %q: %w", endpoint, err)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Treat unresolvable as non-loopback (fail closed).
		if ip := net.ParseIP(host); ip == nil {
			return fmt.Errorf("compressor endpoint %q is not loopback", endpoint)
		}
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return fmt.Errorf("compressor endpoint %q resolves to non-loopback %s", endpoint, ip)
		}
	}
	if len(ips) == 0 {
		if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() {
			return fmt.Errorf("compressor endpoint %q is not loopback", endpoint)
		}
	}
	return nil
}

// validateCompressorResponse parses and validates every field of a plug-in
// response. It does not trust the plug-in's token counts as authoritative.
func validateCompressorResponse(data []byte) (*CompressionResponse, error) {
	var r CompressionResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("invalid compressor response: %w", err)
	}
	if r.Protocol != CompressorProtocol {
		return nil, fmt.Errorf("unexpected compressor protocol %q", r.Protocol)
	}
	if r.Text == "" {
		return nil, fmt.Errorf("compressor response missing text")
	}
	switch r.LossClass {
	case "", "lossless", "selective", "lossy":
	default:
		return nil, fmt.Errorf("invalid compressor loss_class %q", r.LossClass)
	}
	return &r, nil
}

// Registry holds configured compressor plug-ins.
type Registry struct {
	mu          sync.Mutex
	compressors map[string]Compressor
}

// BuildRegistry instantiates compressors from configuration. Disabled or
// unsupported transports are skipped.
func BuildRegistry(cfgs map[string]config.CompressorConfig) *Registry {
	r := &Registry{compressors: map[string]Compressor{}}
	for name, cfg := range cfgs {
		if !cfg.Enabled {
			continue
		}
		switch cfg.Transport {
		case "http", "unix":
			r.compressors[name] = newHTTPCompressor(name, cfg)
		default:
			// Unknown transport is ignored (no shell execution).
		}
	}
	return r
}

// Get returns a compressor by name.
func (r *Registry) Get(name string) (Compressor, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.compressors[name]
	return c, ok
}

// List returns the names of configured compressors.
func (r *Registry) List() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.compressors))
	for n := range r.compressors {
		out = append(out, n)
	}
	return out
}

// Healthy reports whether the named compressor passes a health check.
func (r *Registry) Healthy(ctx context.Context, name string) bool {
	c, ok := r.Get(name)
	if !ok {
		return false
	}
	return c.Health(ctx) == nil
}
