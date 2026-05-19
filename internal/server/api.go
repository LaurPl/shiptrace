package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/LaurPl/shiptrace/internal/store"
)

// defaultLookback is the time window used by every endpoint that doesn't
// take an explicit one. 30 days is "long enough to spot trends, short
// enough to keep the dashboard snappy on a fresh install."
const defaultLookback = 30 * 24 * time.Hour

// writeJSON marshals v and writes it with the right content-type and a
// short cache-control (the dashboard polls; we don't want stale data).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits a uniform error JSON shape. The msg argument is the
// client-facing string; for internal errors callers should use writeInternalError
// instead so the underlying error is logged server-side rather than reflected
// to the network.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

// writeInternalError logs err with context server-side and returns a generic
// "internal error" response. Use this instead of writeError(w, 500, err.Error())
// to avoid leaking SQLite or filesystem error strings to whoever can reach the
// API (which, under --listen-public, can be anyone on the network).
func writeInternalError(w http.ResponseWriter, r *http.Request, where string, err error) {
	log.Printf("shiptrace server: %s %s: %s: %v", r.Method, r.URL.Path, where, err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal error"})
}

// parseDays reads ?days=N from the request (default 30, capped at 365).
func parseDays(r *http.Request) int {
	q := r.URL.Query().Get("days")
	if q == "" {
		return int(defaultLookback / (24 * time.Hour))
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return int(defaultLookback / (24 * time.Hour))
	}
	if n > 365 {
		n = 365
	}
	return n
}

// handleVersion is a trivial liveness probe used by the dashboard footer.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":         "shiptrace",
		"startup":      s.startup.Format(time.RFC3339),
		"uptime_secs":  int(time.Since(s.startup).Seconds()),
		"api_version":  1,
		"schema_state": "live",
	})
}

// hoursRange formats a window for log lines / error messages.
func hoursRange(days int) string {
	return fmt.Sprintf("last %d day(s)", days)
}

// phantomFilter returns the SQL fragment that excludes phantom sessions
// from aggregate queries. The empty string is returned when the caller
// has opted into seeing phantoms via `?include_phantoms=1`. See
// store.PhantomFilterSQL for the rationale.
func phantomFilter(r *http.Request) string {
	if r.URL.Query().Get("include_phantoms") == "1" {
		return ""
	}
	return store.PhantomFilterSQL
}
