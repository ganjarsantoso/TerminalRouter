package console

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
)

// embedFS abstracts the embedded asset filesystem so tests can use os.DirFS.
type embedFS interface {
	Open(name string) (fs.File, error)
}

type noEmbedFS struct{}

func (noEmbedFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

// securityHeaders applies the Console baseline security headers.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if r.URL.Path == "/" || stringsHasSuffix(r.URL.Path, ".html") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":      code,
			"message":   message,
			"request_id": "",
		},
	})
}

func writeConflict(w http.ResponseWriter, expected, current int64) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": map[string]any{
			"code":    "configuration_conflict",
			"message": "Configuration changed in another session.",
			"details": map[string]any{
				"expected_revision": expected,
				"current_revision":  current,
			},
			"actions": []map[string]string{{"label": "Reload configuration", "action": "reload"}},
		},
	})
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return fmt.Errorf("empty body")
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return fmt.Errorf("empty body")
	}
	return json.Unmarshal(b, v)
}

func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func fmtSprintf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

type consoleError struct {
	msg string
}

func errConflict(msg string) error { return &consoleError{msg} }
func errNotFound(kind string) error { return &consoleError{msg: kind + " not found"} }

func (e *consoleError) Error() string { return e.msg }

func getEnv(k string) string {
	return os.Getenv(k)
}

// openBrowser attempts to open the default browser (best effort).
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
