package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireLoopbackHost(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		wantCode int
	}{
		{"127.0.0.1 no port", "127.0.0.1", http.StatusOK},
		{"127.0.0.1 with port", "127.0.0.1:7777", http.StatusOK},
		{"localhost no port", "localhost", http.StatusOK},
		{"localhost with port", "localhost:7777", http.StatusOK},
		{"::1 bracketed with port", "[::1]:7777", http.StatusOK},
		{"::1 bare", "::1", http.StatusOK},
		{"evil.com", "evil.com", http.StatusMisdirectedRequest},
		{"shiptrace.localhost.evil.com", "shiptrace.localhost.evil.com", http.StatusMisdirectedRequest},
		{"192.168.1.10:7777", "192.168.1.10:7777", http.StatusMisdirectedRequest},
		{"empty host", "", http.StatusMisdirectedRequest},
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := requireLoopbackHost(next)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/anything", nil)
			req.Host = tc.host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("host %q: got %d, want %d (body=%s)", tc.host, w.Code, tc.wantCode, w.Body.String())
			}
		})
	}
}

func TestHandlerWrapsMiddlewareWhenAllowAnyHostFalse(t *testing.T) {
	srv := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "evil.com"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusMisdirectedRequest {
		t.Errorf("expected 421 for non-loopback host via Handler(), got %d", w.Code)
	}
}

func TestHandlerBypassesMiddlewareWhenAllowAnyHostTrue(t *testing.T) {
	// Re-construct a server with AllowAnyHost true so we can exercise the
	// public-bind path without involving the cobra command.
	dbSrv := newServer(t)
	srv, err := New(Options{Store: dbSrv.store, AllowAnyHost: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "192.168.1.10:7777"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with AllowAnyHost=true, got %d", w.Code)
	}
}

func TestSecurityHeadersOnApi(t *testing.T) {
	srv := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if got := w.Header().Get("Content-Security-Policy"); got == "" {
		t.Errorf("CSP header missing")
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("nosniff header: %q", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("referrer-policy: %q", got)
	}
}

func TestSecurityHeadersOnHostRejection(t *testing.T) {
	// Even on 421s, the security headers should be set — defense in depth in
	// case the error body ever grows interactive content.
	srv := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "evil.com"
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("status: %d", w.Code)
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("CSP missing on rejection response")
	}
}
