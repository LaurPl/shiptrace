// Package server is shiptrace's local dashboard surface. It serves a
// statically-embedded React bundle on localhost:7777 along with five
// JSON endpoints that the dashboard reads.
//
// The server is intentionally bind-loopback by default: shiptrace is
// local-first, and exposing this surface to the network would change
// what privacy guarantees the tool can make.
package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/LaurPl/shiptrace/internal/store"
)

// DefaultAddr is the loopback address the dashboard binds by default.
const DefaultAddr = "127.0.0.1:7777"

// Server is the HTTP front-end. One per process; create with New.
type Server struct {
	store        *store.Store
	addr         string
	mux          *http.ServeMux
	bundle       fs.FS // the embedded web/dist tree (may be empty)
	hasUI        bool
	startup      time.Time
	allowAnyHost bool // set by Options.AllowAnyHost; bypasses the rebinding check
}

// Options bundles construction parameters.
type Options struct {
	Addr   string
	Store  *store.Store
	Bundle fs.FS // optional — pass nil for API-only operation

	// AllowAnyHost disables the loopback-host check. Only set this when the
	// operator has explicitly opted into a public bind via --listen-public.
	// Leaving it false is the local-first default that protects against DNS
	// rebinding.
	AllowAnyHost bool
}

// New constructs a Server but does not start it. ListenAndServe does.
func New(opts Options) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("server: nil store")
	}
	addr := opts.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	s := &Server{
		store:        opts.Store,
		addr:         addr,
		mux:          http.NewServeMux(),
		startup:      time.Now().UTC(),
		allowAnyHost: opts.AllowAnyHost,
	}
	if opts.Bundle != nil {
		// Embedded FS roots are positional: when we //go:embed web/dist,
		// the FS roots at "web/dist". Reroot so the file server can serve
		// "/" → "web/dist/index.html".
		sub, err := fs.Sub(opts.Bundle, "web/dist")
		if err == nil {
			s.bundle = sub
			s.hasUI = true
		}
	}
	s.registerRoutes()
	return s, nil
}

// Addr returns the resolved address (may be useful in tests).
func (s *Server) Addr() string { return s.addr }

// Handler returns the wrapped HTTP handler chain (security headers +
// Host-header guard + mux) that production callers should serve. Tests that
// exercise a single handler in isolation can still hit s.mux directly to
// bypass the middleware.
func (s *Server) Handler() http.Handler {
	var inner http.Handler = s.mux
	if !s.allowAnyHost {
		inner = requireLoopbackHost(inner)
	}
	return applySecurityHeaders(inner)
}

// ListenAndServe blocks; cancel via http.Server.Shutdown if you need
// graceful teardown. For v0.1 the cobra command wires SIGINT/SIGTERM.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}

func (s *Server) registerRoutes() {
	// API surface. Each handler returns JSON with conservative caching
	// (the dashboard polls rather than caches).
	s.mux.HandleFunc("/api/today", s.handleToday)
	s.mux.HandleFunc("/api/distribution", s.handleDistribution)
	s.mux.HandleFunc("/api/replan-heatmap", s.handleReplanHeatmap)
	s.mux.HandleFunc("/api/agent-skill-roi", s.handleAgentSkillROI)
	s.mux.HandleFunc("/api/provider-mix", s.handleProviderMix)
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/version", s.handleVersion)

	// Static bundle. If absent (Go build without `cd web && npm run
	// build` first), we serve a minimal fallback page so the user knows
	// what's missing.
	s.mux.HandleFunc("/", s.handleRoot)
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if !s.hasUI {
		s.renderFallbackPage(w, r)
		return
	}
	// SPA shell: any path that doesn't have an extension falls back to
	// index.html so client-side routing works on deep links.
	if !strings.Contains(r.URL.Path, ".") {
		r.URL.Path = "/"
	}
	http.FileServerFS(s.bundle).ServeHTTP(w, r)
}

func (s *Server) renderFallbackPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8" />
<title>shiptrace — dashboard bundle missing</title>
<style>
body { font-family: ui-monospace, monospace; padding: 2rem; max-width: 60rem; background: #0a0a0a; color: #d0d0d0; }
code { background: #1a1a1a; padding: 0.1em 0.4em; border-radius: 3px; }
a { color: #6ab04c; }
</style>
</head>
<body>
<h1>shiptrace</h1>
<p>The dashboard binary was built without the React bundle. The JSON API is live, but there's no UI to render it.</p>
<p>To build and embed the bundle:</p>
<pre><code>cd web
npm install
npm run build
cd ..
go build -o /tmp/shiptrace ./cmd/shiptrace</code></pre>
<p>API endpoints (try in another tab):</p>
<ul>
<li><a href="/api/today">/api/today</a></li>
<li><a href="/api/distribution">/api/distribution</a></li>
<li><a href="/api/replan-heatmap">/api/replan-heatmap</a></li>
<li><a href="/api/agent-skill-roi">/api/agent-skill-roi</a></li>
<li><a href="/api/provider-mix">/api/provider-mix</a></li>
</ul>
</body>
</html>`)
}

