package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)(\S+)`),
	regexp.MustCompile(`(?i)(x-api-key:\s*)(\S+)`),
	regexp.MustCompile(`(?i)(api[_-]?key["']?\s*[:=]\s*["']?)([a-zA-Z0-9_\-]{8,})`),
	regexp.MustCompile(`tr_live_[a-f0-9]{16,}`),
	regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`),
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-_]{20,}`),
}

// Redact removes secrets from a string for safe logging.
func Redact(s string) string {
	out := s
	for _, re := range secretPatterns {
		out = re.ReplaceAllStringFunc(out, func(m string) string {
			if strings.HasPrefix(m, "tr_live_") || strings.HasPrefix(m, "sk-") {
				if len(m) > 12 {
					return m[:8] + "••••••••"
				}
				return "••••••••"
			}
			sub := re.FindStringSubmatch(m)
			if len(sub) >= 3 {
				return sub[1] + "••••••••"
			}
			return "••••••••"
		})
	}
	return out
}

// Logger is a structured logger with redaction.
type Logger struct {
	*slog.Logger
	mu     sync.Mutex
	closer io.Closer
}

// New creates a logger writing to stderr and optionally a file under logsDir.
func New(level string, logsDir string) (*Logger, error) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	var writers []io.Writer
	writers = append(writers, os.Stderr)
	var closer io.Closer
	if logsDir != "" {
		_ = os.MkdirAll(logsDir, 0o700)
		f, err := os.OpenFile(filepath.Join(logsDir, "termrouter.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			writers = append(writers, f)
			closer = f
		}
	}
	w := io.MultiWriter(writers...)
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString {
				a.Value = slog.StringValue(Redact(a.Value.String()))
			}
			return a
		},
	})
	return &Logger{Logger: slog.New(handler), closer: closer}, nil
}

func (l *Logger) Close() error {
	if l.closer != nil {
		return l.closer.Close()
	}
	return nil
}

type ctxKey struct{}

// WithRequestID attaches a request ID to the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// RequestIDFrom returns the request ID from context, if any.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
