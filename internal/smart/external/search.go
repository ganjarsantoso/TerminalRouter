package external

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/config"
)

// DefaultSearcher returns the default live web searcher (DuckDuckGo HTML,
// no API key required). It is used by NewService when no searcher is injected.
func DefaultSearcher() Searcher {
	return NewWebSearcher(config.WebSearchConfig{})
}

// defaultFallbackEndpoints are public SearXNG instances tried (in order) when
// the primary endpoint fails. They are infrastructure alternatives, not model
// data, and exist only so the feature keeps working when the default engine is
// blocked (e.g. country-level DuckDuckGo bans). These are not assumed to be
// reachable from any given network; users can override via config.
var defaultFallbackEndpoints = []string{
	"https://searx.be/search",
	"https://priv.au/search",
	"https://search.inetol.net/search",
	"https://baresearch.org/search",
	"https://search.rhscz.eu/search",
	"https://search.bus-hit.me/search",
	"https://searx.tiekoetter.com/search",
}

// NewWebSearcher builds a WebSearcher from configuration. It honors a custom
// endpoint, an optional proxy, and insecure_skip_verify (for TLS-intercepting
// proxies). There is no hardcoded model or engine; the default endpoint is
// DuckDuckGo's public HTML endpoint. The TERMROUTER_WEBSEARCH_INSECURE=1
// environment variable also enables insecure_skip_verify regardless of config.
func NewWebSearcher(cfg config.WebSearchConfig) *WebSearcher {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	insecure := cfg.InsecureSkipVerify || os.Getenv("TERMROUTER_WEBSEARCH_INSECURE") == "1"
	transport := &http.Transport{
		// Picky transparent proxies often drop keep-alive/idle connections,
		// surfacing as EOF. Disable keep-alives and HTTP/2 to be safe.
		DisableKeepAlives: true,
		ForceAttemptHTTP2: false,
	}
	if cfg.Proxy != "" {
		if pu, err := url.Parse(cfg.Proxy); err == nil {
			transport.Proxy = http.ProxyURL(pu)
		}
	}
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		log.Printf("[external] web search TLS verification disabled (insecure_skip_verify); this is for TLS-intercepting proxies only")
	}
	return &WebSearcher{
		Client:            &http.Client{Timeout: timeout, Transport: transport},
		Endpoint:          cfg.Endpoint,
		Method:            cfg.Method,
		FallbackEndpoints: cfg.FallbackEndpoints,
		safe:              NewSafeFetcher(DefaultFetchLimits(), &http.Client{Timeout: timeout, Transport: transport}),
	}
}

// WebSearcher performs unauthenticated web searches and returns result
// snippets. The default backend is DuckDuckGo's HTML endpoint.
type WebSearcher struct {
	Client *http.Client
	// Endpoint is the search endpoint; defaults to DuckDuckGo HTML.
	Endpoint string
	// Method is the preferred HTTP method (POST or GET); GET is the fallback.
	Method string
	// FallbackEndpoints are alternative endpoints tried when the primary fails.
	FallbackEndpoints []string
	// safe enforces SSRF and content controls on page fetches (§17).
	safe *SafeFetcher
}

type ddgResult struct {
	Title   string
	Snippet string
	URL     string
}

var ddgLinkRe = regexp.MustCompile(`<a[^>]+class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
var ddgSnipRe = regexp.MustCompile(`<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)

// Search queries the web backend, then enriches the top results by fetching
// their pages and extracting benchmark-relevant lines. This yields far more
// accurate figures than search snippets alone.
//
// The default method is POST (DuckDuckGo HTML native). If POST fails, GET is
// attempted as a fallback. A custom endpoint and method may be supplied via
// config (Method field); when set, that method is tried first.
//
// If the primary endpoint fails entirely (e.g. country-level block), the
// searcher falls back to configured or built-in alternative endpoints so the
// feature keeps working. Each endpoint tries its primary method, then the
// other method as a last resort.
func (w *WebSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	primary := http.MethodPost
	secondary := http.MethodGet
	if w.Method == http.MethodGet {
		primary, secondary = http.MethodGet, http.MethodPost
	}

	endpoint := w.Endpoint
	if endpoint == "" {
		endpoint = "https://html.duckduckgo.com/html/"
	}

	endpoints := []string{endpoint}
	if w.FallbackEndpoints != nil {
		endpoints = append(endpoints, w.FallbackEndpoints...)
	} else if w.Endpoint == "" {
		endpoints = append(endpoints, defaultFallbackEndpoints...)
	}

	var lastErr error
	for _, ep := range endpoints {
		results, err := w.searchOnce(ctx, client, ep, query, primary)
		if err != nil {
			if r2, err2 := w.searchOnce(ctx, client, ep, query, secondary); err2 == nil {
				return r2, nil
			}
			lastErr = err
			continue
		}
		return results, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no search endpoint configured")
	}
	log.Printf("[external] web search all endpoints failed: %v", lastErr)
	return nil, fmt.Errorf("all search endpoints failed: %w", lastErr)
}

// isTransient reports whether an error is worth retrying (proxy dropped the
// connection, etc.) rather than a definitive failure.
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "connection closed") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "server closed") ||
		strings.Contains(msg, "broken pipe")
}

func (w *WebSearcher) searchOnce(ctx context.Context, client *http.Client, endpoint, query, method string) ([]SearchResult, error) {
	var bodyReader io.Reader
	if method == http.MethodPost {
		bodyReader = strings.NewReader("q=" + url.QueryEscape(query))
	} else {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		endpoint = endpoint + sep + "q=" + url.QueryEscape(query)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) TermRouter/1.0")
	// Force the proxy to close the connection after the response (avoids EOF on
	// reused keep-alive connections behind transparent proxies).
	req.Header.Set("Connection", "close")

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if isTransient(err) {
				continue // retry
			}
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
			msg := strings.TrimSpace(string(body))
			if len(msg) > 500 {
				msg = msg[:500]
			}
			log.Printf("[external] web search %s %s returned %d: %s", method, endpoint, resp.StatusCode, msg)
			return nil, fmt.Errorf("search endpoint %s returned %d: %s", endpoint, resp.StatusCode, msg)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			if isTransient(err) {
				continue
			}
			return nil, err
		}
		results := parseDuckDuckGo(body)
		// Enrich top results with page text (best-effort, bounded).
		const enrichLimit = 6
		for i, r := range results {
			if i >= enrichLimit || r.URL == "" {
				continue
			}
			if pageText, err := w.FetchPage(r.URL); err == nil && pageText != "" {
				results[i].Snippet = r.Snippet + " " + pageText
			}
		}
		return results, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("search failed after retries")
}

// FetchPage implements PageFetcher: fetches a page and returns benchmark-
// relevant lines (those containing a percentage or Elo near a benchmark
// keyword), truncated. All fetches are subject to SSRF and content controls
// (§17) via the embedded SafeFetcher; a keyless reader fallback is used only
// when the direct fetch is blocked.
func (w *WebSearcher) FetchPage(pageURL string) (string, error) {
	sf := w.safe
	if sf == nil {
		sf = NewSafeFetcher(DefaultFetchLimits(), w.Client)
	}
	if text, err := sf.FetchPage(pageURL); err == nil {
		return text, nil
	}
	// Fallback: Jina AI reader (keyless) cleans and returns the page text. It
	// is itself SSRF-checked as an approved reader host.
	return sf.FetchPage(fetchViaJinaURL(pageURL))
}

// fetchViaJinaURL returns the Jina reader URL for a target page (approved reader
// host; still SSRF-checked by SafeFetcher).
func fetchViaJinaURL(pageURL string) string {
	return "https://s.jina.ai/" + pageURL
}

// keepBenchmarkLines filters page text down to lines that mention a benchmark
// keyword together with a percentage or Elo figure, truncated to a cap.
func keepBenchmarkLines(text string) string {
	var keep []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 4 || len(line) > 300 {
			continue
		}
		low := strings.ToLower(line)
		if strings.Contains(low, "%") || strings.Contains(low, "elo") {
			if benchmarkWord.MatchString(low) {
				keep = append(keep, line)
			}
		}
		if len(keep) >= 20 {
			break
		}
	}
	return strings.Join(keep, " ")
}

var benchmarkWord = regexp.MustCompile(`(?i)livebench|swe[- ]?bench|intelligence index|gpqa|mmlu|math-?500|humaneval|ifeval|mmmu|arena|elo|benchmark`)

func parseDuckDuckGo(body []byte) []SearchResult {
	var out []SearchResult
	links := ddgLinkRe.FindAllStringSubmatch(string(body), -1)
	snips := ddgSnipRe.FindAllStringSubmatch(string(body), -1)
	for i, l := range links {
		url := decodeDuckDuckGoURL(l[1])
		title := stripTags(l[2])
		snippet := ""
		if i < len(snips) {
			snippet = stripTags(snips[i][1])
		}
		out = append(out, SearchResult{
			Title:   html.UnescapeString(title),
			Snippet: html.UnescapeString(snippet),
			URL:     url,
		})
	}
	return out
}

// DuckDuckGo wraps redirect URLs as //duckduckgo.com/l/?uddg=<encoded>.
func decodeDuckDuckGoURL(raw string) string {
	if strings.Contains(raw, "uddg=") {
		if i := strings.Index(raw, "uddg="); i >= 0 {
			enc := raw[i+5:]
			if j := strings.IndexAny(enc, "&#"); j >= 0 {
				enc = enc[:j]
			}
			if dec, err := url.QueryUnescape(enc); err == nil {
				return dec
			}
		}
	}
	return raw
}

func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return strings.TrimSpace(re.ReplaceAllString(s, " "))
}
