package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/termrouter/termrouter/internal/observability"
)

// diagnosticsHTTPClient is an explicit HTTP client for local CLI diagnostics. It
// has a 30-second total timeout, refuses redirects to unexpected hosts, and is
// replaceable for testing.
var diagnosticsHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

const maxErrorBody = 4 << 10 // 4KB

func httpNewRequest(ctx context.Context, method, url, body string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
}

func httpDo(req *http.Request) (*http.Response, error) {
	return diagnosticsHTTPClient.Do(req)
}

// readResponseBody reads the full body. For error responses (status >= 400),
// reading is bounded to maxErrorBody to avoid unbounded memory use.
func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return readBounded(resp.Body, maxErrorBody)
	}
	return io.ReadAll(resp.Body)
}

func readBounded(r io.Reader, limit int) ([]byte, error) {
	lr := io.LimitReader(r, int64(limit))
	b, err := io.ReadAll(lr)
	if err != nil {
		return b, err
	}
	if len(b) == limit {
		// Check if there is more data (truncation signal).
		var buf [1]byte
		n, _ := r.Read(buf[:])
		if n > 0 {
			b = append(b, []byte(fmt.Sprintf("\n... (%d+ bytes truncated)", limit))...)
		}
	}
	return b, nil
}

func observabilityQuiet() (*observability.Logger, error) {
	return observability.New("error", "")
}
