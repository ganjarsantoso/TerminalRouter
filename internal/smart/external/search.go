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

// NewWebSearcher builds a WebSearcher from configuration. It honors a custom
// endpoint, an optional proxy, and insecure_skip_verify (for TLS-intercepting
// proxies). There is no hardcoded model or engine; the default endpoint is
// DuckDuckGo's public HTML endpoint. The TERMROUTER_WEBSEARCH_INSECURE=1
// environment variable also enables insecure_skip_verify regardless of config.
func NewWebSearcher(cfg config.WebSearchConfig) *WebSearcher {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	insecure := cfg.InsecureSkipVerify || os.Getenv("TERMROUTER_WEBSEARCH_INSECURE") == "1"
	transport := &http.Transport{}
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
		Client:   &http.Client{Timeout: timeout, Transport: transport},
		Endpoint: cfg.Endpoint,
	}
}

// WebSearcher performs unauthenticated web searches and returns result
// snippets. The default backend is DuckDuckGo's HTML endpoint.
type WebSearcher struct {
	Client *http.Client
	// Endpoint is the search endpoint; defaults to DuckDuckGo HTML.
	Endpoint string
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
func (w *WebSearcher) Search(ctx context.Context, query string) ([]SearchResult, error) {
	endpoint := w.Endpoint
	if endpoint == "" {
		endpoint = "https://html.duckduckgo.com/html/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("q="+url.QueryEscape(query)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "TermRouter/1.0 (+https://github.com/termrouter/termrouter)")

	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
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

// FetchPage implements PageFetcher: fetches a page and returns benchmark-
// relevant lines (those containing a percentage or Elo near a benchmark
// keyword), truncated.
func (w *WebSearcher) FetchPage(pageURL string) (string, error) {
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return w.fetchPageText(context.Background(), client, pageURL)
}

// fetchPageText fetches a page and returns benchmark-relevant lines (those
// containing a percentage or Elo near a benchmark keyword), truncated.
func (w *WebSearcher) fetchPageText(ctx context.Context, client *http.Client, pageURL string) (string, error) {
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
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	text := stripTags(string(raw))
	// Keep only lines that mention a benchmark keyword and a number.
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
	return strings.Join(keep, " "), nil
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
