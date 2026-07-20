package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(token string) *Server {
	return &Server{
		sessions:       map[string]session{},
		bootstrapToken: token,
	}
}

func TestHandleBootstrapJSON(t *testing.T) {
	s := newTestServer("secret")
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/session/bootstrap", strings.NewReader(`{"token":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleBootstrap(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("JSON: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleBootstrapJSONCharset(t *testing.T) {
	s := newTestServer("secret")
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/session/bootstrap", strings.NewReader(`{"token":"secret"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	w := httptest.NewRecorder()
	s.handleBootstrap(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("JSON+charset: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleBootstrapForm(t *testing.T) {
	s := newTestServer("secret")
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/session/bootstrap", strings.NewReader("token=secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleBootstrap(w, req)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("form: expected 307 redirect, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleBootstrapWrongToken(t *testing.T) {
	s := newTestServer("secret")
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/session/bootstrap", strings.NewReader(`{"token":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleBootstrap(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", w.Code)
	}
}
