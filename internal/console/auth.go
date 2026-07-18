package console

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"
)

// sessionCookieName is the admin session cookie.
const sessionCookieName = "termrouter_admin_session"

// csrfCookieName carries the CSRF token (doubles as double-submit cookie).
const csrfCookieName = "termrouter_admin_csrf"

const (
	sessionIdleTimeout = 30 * time.Minute
	sessionAbsTimeout  = 8 * time.Hour
)

type session struct {
	ID        string
	CreatedAt time.Time
	LastSeen  time.Time
}

// generateSessionToken returns a cryptographically random session token.
func generateSessionToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// generateCSRFToken returns a cryptographically random CSRF token.
func generateCSRFToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// handleBootstrap exchanges a one-time bootstrap token for an admin session.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	var token string
	ct := r.Header.Get("Content-Type")
	if len(ct) >= 19 && ct[:19] == "application/json" {
		var body struct {
			Token string `json:"token"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		token = body.Token
	} else {
		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
			return
		}
		token = r.FormValue("token")
	}
	s.mu.Lock()
	expected := s.bootstrapToken
	s.mu.Unlock()
	if expected == "" || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_token", "bootstrap token invalid or already used")
		return
	}
	// Invalidate bootstrap token immediately.
	s.mu.Lock()
	s.bootstrapToken = ""
	sid := generateSessionToken()
	s.sessions[sid] = session{ID: sid, CreatedAt: time.Now(), LastSeen: time.Now()}
	s.currentSession = sid
	s.mu.Unlock()

	csrf := generateCSRFToken()
	s.setSessionCookie(w, sid)
	s.setCSRFCookie(w, csrf)

	// If form-encoded (no JS), redirect to the console. Otherwise return JSON for fetch().
	if ct := r.Header.Get("Content-Type"); len(ct) >= 19 && ct[:19] == "application/json" {
		writeJSON(w, http.StatusOK, map[string]any{
			"session":    map[string]any{"id": sid},
			"csrf":       csrf,
			"csrf_token": csrf,
		})
	} else {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

// handleLogin renders a tiny HTML page that posts the token to bootstrap.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	expected := s.bootstrapToken
	s.mu.RUnlock()
	if expected == "" || expected != token {
		http.Error(w, "This login link has expired or was already used. Restart the Console to generate a new one.", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>TermRouter Console — Sign in</title>
<style>body{font-family:system-ui,sans-serif;background:#0f1115;color:#e5e7eb;display:flex;height:100vh;align-items:center;justify-content:center;margin:0}
.card{background:#171a21;border:1px solid #23262f;border-radius:12px;padding:32px;max-width:380px;text-align:center}
h1{font-size:18px;margin:0 0 8px}a.btn{display:inline-block;margin-top:16px;padding:10px 18px;background:#6366f1;color:#fff;border-radius:8px;text-decoration:none;font-weight:600}
p{color:#9ca3af;font-size:13px}</style></head>
<body><div class="card"><h1>TermRouter Console</h1><p>You are about to sign in to the local management console. This link is single-use.</p>
<form method="post" action="/admin/v1/session/bootstrap"><input type="hidden" name="token" value="` + token + `"><button class="btn" type="submit">Sign in</button></form></div>
<script>document.querySelector('form').addEventListener('submit',async e=>{e.preventDefault();const t=document.querySelector('input').value;const r=await fetch('/admin/v1/session/bootstrap',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({token:t})});if(r.ok){window.location.href='/';}else{alert('Sign in failed');}});</script>
</body></html>`))
}

// requireSession wraps a handler that needs a valid admin session.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sid, ok := s.validSession(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "admin session required")
			return
		}
		s.mu.Lock()
		if sess, ok := s.sessions[sid]; ok {
			sess.LastSeen = time.Now()
			s.sessions[sid] = sess
		}
		s.mu.Unlock()
		next(w, r)
	}
}

// requireSessionMaybe allows unauthenticated access only to /login, bootstrap, csrf, and static assets.
func (s *Server) requireSessionMaybe(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || r.URL.Path == "/admin/v1/session/bootstrap" || r.URL.Path == "/admin/v1/csrf" {
			h.ServeHTTP(w, r)
			return
		}
		if stringsHasPrefix(r.URL.Path, "/admin/v1") {
			sid, ok := s.validSession(r)
			if !ok {
				writeError(w, http.StatusUnauthorized, "unauthenticated", "admin session required")
				return
			}
			s.mu.Lock()
			if sess, ok := s.sessions[sid]; ok {
				sess.LastSeen = time.Now()
				s.sessions[sid] = sess
			}
			s.mu.Unlock()
			h.ServeHTTP(w, r)
			return
		}
		// Static SPA: require session so the app shell is not exposed publicly.
		if _, ok := s.validSession(r); !ok {
			// Serve a lightweight guidance page (bootstrap URL lives in the terminal).
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><title>TermRouter Console</title>
<style>body{font-family:system-ui,sans-serif;background:#0b1220;color:#e5e7eb;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
.card{background:#111827;border:1px solid #1f2937;border-radius:16px;padding:36px;max-width:420px;text-align:center;box-shadow:0 20px 50px rgba(0,0,0,.4)}
h1{font-size:20px;margin:0 0 10px;color:#67e8f9}p{color:#9ca3af;font-size:14px;line-height:1.5}code{background:#0b1220;padding:2px 6px;border-radius:6px;color:#a5f3fc}</style></head>
<body><div class="card"><h1>TermRouter Console</h1>
<p>Open the one-time login URL printed in your terminal when you ran <code>termrouter console</code>.</p>
<p>That link is single-use and authenticates this local browser session.</p>
</div></body></html>`))
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) validSession(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[c.Value]
	if !ok {
		return "", false
	}
	now := time.Now()
	if now.Sub(sess.CreatedAt) > sessionAbsTimeout || now.Sub(sess.LastSeen) > sessionIdleTimeout {
		delete(s.sessions, c.Value)
		return "", false
	}
	return c.Value, true
}

// csrfProtected enforces the double-submit CSRF token for state-changing methods.
func (s *Server) csrfProtected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next(w, r)
			return
		}
		c, err := r.Cookie(csrfCookieName)
		if err != nil || c.Value == "" {
			writeError(w, http.StatusForbidden, "csrf_required", "CSRF token missing")
			return
		}
		header := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(header)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_mismatch", "CSRF token mismatch")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleCSRFToken(w http.ResponseWriter, r *http.Request) {
	_, ok := s.validSession(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "admin session required")
		return
	}
	csrf := generateCSRFToken()
	s.setCSRFCookie(w, csrf)
	writeJSON(w, http.StatusOK, map[string]any{"csrf": csrf, "csrf_token": csrf})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sid, _ := s.validSession(r)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "session_id": sid})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	sid, _ := s.validSession(r)
	s.mu.Lock()
	delete(s.sessions, sid)
	s.currentSession = ""
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Path: "/", MaxAge: -1, HttpOnly: false, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"logged_out": true})
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: sid, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		MaxAge: int(sessionAbsTimeout.Seconds()), Secure: false,
	})
}

func (s *Server) setCSRFCookie(w http.ResponseWriter, csrf string) {
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookieName, Value: csrf, Path: "/",
		HttpOnly: false, SameSite: http.SameSiteStrictMode,
		MaxAge: int(sessionAbsTimeout.Seconds()), Secure: false,
	})
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
