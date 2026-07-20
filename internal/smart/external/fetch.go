package external

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchLimits bounds a single evidence page fetch (§17 content controls).
type FetchLimits struct {
	MaxBytes      int64         // max decompressed body size (0 = default)
	MaxRedirects  int           // max redirect hops (0 = default)
	ConnectTimeout time.Duration // (reserved) dial timeout
	HeaderTimeout  time.Duration // (reserved) header timeout
	TotalTimeout   time.Duration // overall request timeout
}

// DefaultFetchLimits returns safe defaults for evidence fetching.
func DefaultFetchLimits() FetchLimits {
	return FetchLimits{
		MaxBytes:     2 << 20, // 2 MiB
		MaxRedirects: 4,
		TotalTimeout: 20 * time.Second,
	}
}

// jinaReaderHost is the keyless reader used as a fallback when a page is
// unreachable directly. It is itself an approved egress target (it only returns
// cleaned text), but the underlying page still must have come from an approved
// domain for scoring weight (enforced by IsApprovedURL).
const jinaReaderHost = "s.jina.ai"

// ValidateFetchURL enforces the SSRF protections required for untrusted page
// fetches (§17): only http/https, and the resolved destination must not be a
// loopback, private, link-local, or cloud metadata IP. Approved-domain gating
// for scoring is enforced separately by IsApprovedURL.
func ValidateFetchURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		// Allow the request to proceed to surface a clearer network error, but
		// if resolution fails we cannot SSRF-check; reject to be safe.
		return nil, fmt.Errorf("cannot resolve host %q: %w", host, err)
	}
	for _, ip := range ips {
		if isRestrictedIP(ip) {
			return nil, fmt.Errorf("refusing fetch to restricted address %s for host %q", ip, host)
		}
	}
	return u, nil
}

// isRestrictedIP reports whether ip is loopback, private, link-local, or a
// cloud metadata address (e.g. 169.254.169.254).
func isRestrictedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Cloud metadata (169.254.0.0/16) is link-local, but be explicit.
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	return false
}

// IsApprovedURL reports whether the page URL is eligible to contribute automatic
// scoring weight (§15): empty (unverified but allowed), an approved source
// domain, or the Jina reader fallback.
func IsApprovedURL(rawURL string) bool {
	if rawURL == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if host == jinaReaderHost || strings.HasSuffix(host, "."+jinaReaderHost) {
		return true
	}
	return IsApprovedHost(host)
}

// SafeFetcher fetches benchmark pages with SSRF and content controls.
type SafeFetcher struct {
	Limits FetchLimits
	Client *http.Client
}

// NewSafeFetcher builds a SafeFetcher. If client is nil, a default client with
// redirect limiting and timeouts is used.
func NewSafeFetcher(limits FetchLimits, client *http.Client) *SafeFetcher {
	if limits.MaxBytes == 0 {
		limits.MaxBytes = DefaultFetchLimits().MaxBytes
	}
	if limits.MaxRedirects == 0 {
		limits.MaxRedirects = DefaultFetchLimits().MaxRedirects
	}
	if limits.TotalTimeout == 0 {
		limits.TotalTimeout = DefaultFetchLimits().TotalTimeout
	}
	if client == nil {
		client = &http.Client{
			Timeout: limits.TotalTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > limits.MaxRedirects {
					return fmt.Errorf("too many redirects")
				}
				// Redirects must stay within approved domains or the reader host.
				if !IsApprovedURL(req.URL.String()) {
					return fmt.Errorf("redirect to non-approved domain rejected")
				}
				return nil
			},
		}
	}
	return &SafeFetcher{Limits: limits, Client: client}
}

// FetchPage implements PageFetcher: it SSRF-validates the URL, enforces a
// content-type allowlist and body-size cap, and returns cleaned text.
func (f *SafeFetcher) FetchPage(pageURL string) (string, error) {
	if _, err := ValidateFetchURL(pageURL); err != nil {
		return "", err
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	ctx := context.Background()
	if f.Limits.TotalTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.Limits.TotalTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "TermRouter/1.0 (+https://github.com/termrouter/termrouter)")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("page returned %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !allowedContentType(ct) {
		return "", fmt.Errorf("unsupported content-type %q", ct)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, f.Limits.MaxBytes))
	if err != nil {
		return "", err
	}
	return keepBenchmarkLines(stripTags(string(raw))), nil
}

var allowedContentTypes = []string{
	"text/html",
	"application/json",
	"text/csv",
	"text/plain",
	"application/csv",
}

func allowedContentType(ct string) bool {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if ct == "" {
		return true // be lenient when unspecified
	}
	for _, a := range allowedContentTypes {
		if ct == a {
			return true
		}
	}
	return false
}

// ValidateEvidenceRecord enforces the extraction schema rules (§16): reject a
// record when its source is unknown, benchmark is missing, raw score is
// missing/invalid, scale is unknown, or the raw value is outside the native
// range. Records from non-approved URLs are not rejected here (they are gated
// for scoring weight by the caller via IsApprovedURL) but are reported via the
// returned error's Unverified flag.
type ValidationResult struct {
	OK        bool
	Unverified bool
	Reason    string
}

// ValidateEvidenceRecord checks a single evidence record against the registry
// and native-scale ranges.
func ValidateEvidenceRecord(rec EvidenceRecord) ValidationResult {
	if _, ok := sourceMetaByID(rec.Source); !ok {
		return ValidationResult{Reason: "unknown source " + string(rec.Source)}
	}
	if rec.Benchmark == "" {
		return ValidationResult{Reason: "missing benchmark id"}
	}
	if rec.Scale == "" {
		return ValidationResult{Reason: "unknown unit/scale"}
	}
	if rec.Value <= 0 || isNaN(rec.Value) {
		return ValidationResult{Reason: "missing or invalid raw score"}
	}
	if !nativeRangeOK(rec.Scale, rec.Value) {
		return ValidationResult{Reason: "raw score outside native range"}
	}
	verified := IsApprovedURL(rec.URL)
	if !verified {
		return ValidationResult{OK: true, Unverified: true, Reason: "source url not in approved registry; contributes no automatic weight"}
	}
	return ValidationResult{OK: true}
}

func isNaN(v float64) bool {
	return v != v
}

// nativeRangeOK reports whether a raw value is plausible on its native scale.
func nativeRangeOK(scale ScaleKind, v float64) bool {
	switch scale {
	case ScaleZeroToHundred, ScaleZeroToTen, ScaleZeroToOne:
		return v >= 0
	case ScaleElo:
		return v > 0 && v < 4000
	default:
		return false
	}
}
