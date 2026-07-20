package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type clientIPCtx struct{}

// ClientIPFrom returns the request client IP, preferring a trusted reverse-proxy
// derived address when available.
func ClientIPFrom(ctx context.Context) string {
	if v, ok := ctx.Value(clientIPCtx{}).(string); ok {
		return v
	}
	return ""
}

// ClientLabelFrom returns an optional non-secret attribution label.
func ClientLabelFrom(ctx context.Context) string {
	if v, ok := ctx.Value(clientLabelCtx{}).(string); ok {
		return v
	}
	return ""
}

type clientLabelCtx struct{}

// TrustedProxy rewrites RemoteAddr from X-Forwarded-For / X-Real-IP only when
// the immediate peer is in the configured trusted proxy CIDR list.
type TrustedProxy struct {
	// Networks are pre-parsed trusted proxy networks.
	Networks []*net.IPNet
}

// ParseTrustedProxies parses CIDR or bare-IP entries into networks.
func ParseTrustedProxies(entries []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if !strings.Contains(e, "/") {
			ip := net.ParseIP(e)
			if ip == nil {
				return nil, &net.ParseError{Type: "IP address", Text: e}
			}
			if ip.To4() != nil {
				e = ip.String() + "/32"
			} else {
				e = ip.String() + "/128"
			}
		}
		_, n, err := net.ParseCIDR(e)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// Middleware derives client IP and optional client label.
func (t *TrustedProxy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := directPeerIP(r)
		if t != nil && len(t.Networks) > 0 && ipTrusted(ip, t.Networks) {
			if forwarded := firstForwardedIP(r); forwarded != "" {
				ip = forwarded
			}
		}
		ctx := context.WithValue(r.Context(), clientIPCtx{}, ip)

		// Optional non-secret device attribution (never used for authorization).
		label := strings.TrimSpace(r.Header.Get("X-TermRouter-Client-Name"))
		if label == "" {
			label = strings.TrimSpace(r.Header.Get("X-Client-Name"))
		}
		if len(label) > 64 {
			label = label[:64]
		}
		if label != "" {
			ctx = context.WithValue(ctx, clientLabelCtx{}, label)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func directPeerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may already be bare IP in tests.
		return r.RemoteAddr
	}
	return host
}

func ipTrusted(ipStr string, nets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func firstForwardedIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Left-most is the original client when proxies append.
		parts := strings.Split(xff, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if net.ParseIP(p) != nil {
				return p
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}
	return ""
}
