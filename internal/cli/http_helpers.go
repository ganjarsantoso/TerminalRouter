package cli

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/termrouter/termrouter/internal/observability"
)

func httpNewRequest(ctx context.Context, method, url, body string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
}

func httpDo(req *http.Request) (*http.Response, error) {
	return http.DefaultClient.Do(req)
}

func readAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func observabilityQuiet() (*observability.Logger, error) {
	return observability.New("error", "")
}
