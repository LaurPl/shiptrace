package server

import (
	"net"
	"net/http"
	"strings"
)

// loopbackHosts is the closed set of hostnames the dashboard accepts by
// default. Anything else hitting us is either a misconfiguration or — more
// concerning — a DNS-rebinding attempt where an attacker page resolves a
// hostile domain to 127.0.0.1 after load to bypass same-origin policy.
//
// IPv6 literals carry brackets in r.Host; IPv4 and bare names do not.
var loopbackHosts = map[string]struct{}{
	"127.0.0.1": {},
	"localhost": {},
	"[::1]":     {},
	"::1":       {},
}

// securityHeaders is the conservative header set we apply to every response.
// CSP locks the dashboard to self-hosted scripts and styles; nosniff stops
// browsers from MIME-sniffing API responses into something executable;
// no-referrer keeps file paths and project names out of any outbound headers
// if the user ever navigates from the dashboard to an external page.
const (
	cspPolicy       = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self'; object-src 'none'; frame-ancestors 'none'"
	referrerPolicy  = "no-referrer"
	contentTypeOpts = "nosniff"
)

// applySecurityHeaders wraps next so every response (including 4xx/5xx) has
// the baseline headers set. The middleware sets headers BEFORE next runs so
// handlers that call WriteHeader still get them flushed.
func applySecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", cspPolicy)
		h.Set("X-Content-Type-Options", contentTypeOpts)
		h.Set("Referrer-Policy", referrerPolicy)
		next.ServeHTTP(w, r)
	})
}

// requireLoopbackHost wraps next so any request whose Host header isn't a
// loopback literal is rejected with 421 Misdirected Request. We use 421
// rather than 400 because it's specifically for "this server isn't who the
// client thinks it is" — exactly the rebinding case.
func requireLoopbackHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		// Strip the port if present. SplitHostPort fails on bare names, so
		// fall back to the raw value when it does.
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		// IPv6 in Host arrives with brackets even for the bare form ("[::1]");
		// SplitHostPort already strips them when a port is present, but a
		// no-port "[::1]" gets through unchanged. Normalize.
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		if _, ok := loopbackHosts[host]; !ok {
			http.Error(w, "shiptrace: this dashboard refuses non-loopback Host headers (DNS rebinding protection). Got: "+r.Host, http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}
