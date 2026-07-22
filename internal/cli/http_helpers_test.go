package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsClientHasTimeout(t *testing.T) {
	if diagnosticsHTTPClient.Timeout != 30*time.Second {
		t.Fatalf("expected 30s timeout, got %v", diagnosticsHTTPClient.Timeout)
	}
}

func TestDiagnosticsClientRejectsRedirects(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer srv.Close()

	req, err := httpNewRequest(context.Background(), "GET", srv.URL, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := httpDo(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 (redirect not followed), got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDiagnosticsClientTestableTransport(t *testing.T) {
	original := diagnosticsHTTPClient
	defer func() { diagnosticsHTTPClient = original }()

	called := false
	diagnosticsHTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	req, _ := httpNewRequest(context.Background(), "GET", "http://test.local/", "")
	_, err := httpDo(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected transport to be called")
	}
}

func TestReadResponseBodyClosesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer srv.Close()

	req, _ := httpNewRequest(context.Background(), "GET", srv.URL, "")
	resp, err := httpDo(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := readResponseBody(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(b))
	}
}

func TestReadResponseBodyErrorBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(strings.Repeat("x", 10<<10))) // 10KB
	}))
	defer srv.Close()

	req, _ := httpNewRequest(context.Background(), "GET", srv.URL, "")
	resp, err := httpDo(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := readResponseBody(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) > maxErrorBody+100 {
		t.Fatalf("error body too large: %d bytes (limit %d)", len(b), maxErrorBody)
	}
}

func TestReadResponseBodySuccessUnbounded(t *testing.T) {
	data := strings.Repeat("y", 20<<10) // 20KB success body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(data))
	}))
	defer srv.Close()

	req, _ := httpNewRequest(context.Background(), "GET", srv.URL, "")
	resp, err := httpDo(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := readResponseBody(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b) != len(data) {
		t.Fatalf("expected %d bytes, got %d", len(data), len(b))
	}
}

func TestReadBoundedUnderLimit(t *testing.T) {
	b, err := readBounded(strings.NewReader("small"), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != "small" {
		t.Fatalf("expected %q, got %q", "small", string(b))
	}
}

func TestReadBoundedExactLimit(t *testing.T) {
	data := "exact"
	b, err := readBounded(strings.NewReader(data), len(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(b) != data {
		t.Fatalf("expected %q, got %q", data, string(b))
	}
}

func TestReadBoundedOverLimit(t *testing.T) {
	b, err := readBounded(strings.NewReader("this is too long"), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain truncation message.
	if !strings.Contains(string(b), "truncated") {
		t.Fatalf("expected truncation indicator in %q", string(b))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
