package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	srv, err := New(Options{Store: s})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func seedSession(t *testing.T, s *store.Store, id, project, agent, provider string, startTs, endTs int64, shipCount int) {
	t.Helper()
	ctx := context.Background()
	e := events.Event{
		EventType: events.SessionStart,
		Ts:        time.Unix(startTs, 0).UTC(),
		SessionID: id,
		Provider:  provider,
		Project:   project,
		Agent:     agent,
		Label:     "test " + id,
	}.WithDefaults()
	if err := s.UpsertSessionStart(ctx, e); err != nil {
		t.Fatalf("seed start: %v", err)
	}
	if endTs > 0 {
		if err := s.UpdateSessionStop(ctx, events.Event{
			EventType: events.SessionStop,
			Ts:        time.Unix(endTs, 0).UTC(),
			SessionID: id,
		}.WithDefaults()); err != nil {
			t.Fatalf("seed stop: %v", err)
		}
	}
	for i := 0; i < shipCount; i++ {
		if err := s.InsertShipEvent(ctx, events.Event{
			EventType: events.Ship,
			Ts:        time.Unix(endTs+int64(i)+1, 0).UTC(),
			SessionID: id,
			Metadata:  map[string]any{"kind": "manual"},
		}.WithDefaults()); err != nil {
			t.Fatalf("seed ship: %v", err)
		}
	}
}

func TestApiTodayReturnsRecentSessions(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC()
	seedSession(t, srv.store, "shp_recent", "social", "ig-strat", "claude-code", now.Add(-2*time.Hour).Unix(), now.Add(-time.Hour).Unix(), 1)
	seedSession(t, srv.store, "shp_old", "social", "ig-strat", "claude-code", now.Add(-48*time.Hour).Unix(), now.Add(-47*time.Hour).Unix(), 0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/today", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var resp TodayResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Sessions) != 1 || resp.Sessions[0].ID != "shp_recent" {
		t.Errorf("expected 1 recent session, got %+v", resp.Sessions)
	}
	if resp.Sessions[0].ShipCount != 1 {
		t.Errorf("ship_count: %d", resp.Sessions[0].ShipCount)
	}
}

func TestApiDistributionGroupsByProject(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC().Unix()
	seedSession(t, srv.store, "shp_a1", "social", "", "claude-code", now-1000, now-500, 2)
	seedSession(t, srv.store, "shp_a2", "social", "", "claude-code", now-800, now-300, 0)
	seedSession(t, srv.store, "shp_b1", "research", "", "claude-code", now-700, now-200, 1)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/distribution", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var resp DistributionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Projects) != 2 {
		t.Fatalf("projects: %+v", resp.Projects)
	}
	for _, p := range resp.Projects {
		if p.Name == "social" && (p.Sessions != 2 || p.Ships != 2) {
			t.Errorf("social: %+v", p)
		}
		if p.Name == "research" && (p.Sessions != 1 || p.Ships != 1) {
			t.Errorf("research: %+v", p)
		}
	}
}

func TestApiProviderMix(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC().Unix()
	seedSession(t, srv.store, "shp_cc", "x", "", "claude-code", now-1000, now-500, 1)
	seedSession(t, srv.store, "shp_man", "y", "", "manual", now-700, now-200, 0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/provider-mix", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	var resp ProviderMixResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Providers) != 2 {
		t.Errorf("got %d providers", len(resp.Providers))
	}
}

func TestApiAgentSkillROI(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC().Unix()
	seedSession(t, srv.store, "shp_a", "x", "ig-strat", "claude-code", now-1000, now-500, 1)
	seedSession(t, srv.store, "shp_b", "x", "ig-strat", "claude-code", now-700, now-200, 1)
	seedSession(t, srv.store, "shp_c", "x", "", "claude-code", now-500, now-100, 0)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/agent-skill-roi", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	var resp AgentSkillResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.ByAgent) < 2 {
		t.Errorf("byAgent should include ig-strat and (none): %+v", resp.ByAgent)
	}
}

func TestApiReplanHeatmap(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC().Unix()
	seedSession(t, srv.store, "shp_x", "social", "", "claude-code", now-1000, now-500, 0)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/replan-heatmap", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	var resp ReplanHeatmapResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Cells) == 0 {
		t.Errorf("cells empty")
	}
}

// TestApiSessionReturnsDetail seeds a session with a tool_use, a
// replan_signal, and a ship_event, then asserts /api/session/{id}
// returns all three streams in ts order under the right keys.
func TestApiSessionReturnsDetail(t *testing.T) {
	srv := newServer(t)
	ctx := context.Background()
	now := time.Now().UTC().Unix()

	seedSession(t, srv.store, "shp_detail", "social", "ig-strat", "claude-code", now-1000, now-100, 1)

	if err := srv.store.InsertToolEvent(ctx, events.Event{
		EventType:    events.ToolUse,
		Ts:           time.Unix(now-800, 0).UTC(),
		SessionID:    "shp_detail",
		Tool:         "Read",
		FilesTouched: []string{"a.go", "b.go"},
	}.WithDefaults()); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
	if err := srv.store.InsertReplanSignal(ctx, events.Event{
		EventType: events.ReplanSignal,
		Ts:        time.Unix(now-500, 0).UTC(),
		SessionID: "shp_detail",
		Metadata:  map[string]any{"kind": "todo_revoked", "weight": 0.5},
	}.WithDefaults()); err != nil {
		t.Fatalf("seed replan: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/session/shp_detail", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var resp SessionDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Session.ID != "shp_detail" {
		t.Errorf("session.id: %q", resp.Session.ID)
	}
	if len(resp.ToolEvents) != 1 || resp.ToolEvents[0].Tool != "Read" {
		t.Errorf("tool_events: %+v", resp.ToolEvents)
	}
	if got := resp.ToolEvents[0].FilesTouched; len(got) != 2 || got[0] != "a.go" {
		t.Errorf("files_touched round-trip: %+v", got)
	}
	if len(resp.ReplanSignals) != 1 || resp.ReplanSignals[0].Kind != "todo_revoked" {
		t.Errorf("replan_signals: %+v", resp.ReplanSignals)
	}
	if len(resp.ShipEvents) != 1 {
		t.Errorf("ship_events: %+v", resp.ShipEvents)
	}
}

func TestApiSessionUnknownReturns404(t *testing.T) {
	srv := newServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/session/does-not-exist", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestApiHealthReportsShipPresence(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC().Unix()

	// Empty store: no ships.
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var resp HealthResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.HasShips {
		t.Errorf("expected has_ships=false on empty store, got %+v", resp)
	}

	// Add a ship and recheck.
	seedSession(t, srv.store, "shp_x", "p", "", "claude-code", now-1000, now-500, 1)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.HasShips {
		t.Errorf("expected has_ships=true after seeding a ship, got %+v", resp)
	}
}

func TestApiVersion(t *testing.T) {
	srv := newServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
}

// TestInternalErrorDoesNotLeakUnderlying confirms that when a handler hits
// the store after we've closed it, the response carries the generic
// "internal error" string — not the SQLite driver's error text. The
// underlying detail still goes to the test logger so debugging stays cheap.
func TestInternalErrorDoesNotLeakUnderlying(t *testing.T) {
	srv := newServer(t)
	// Pump the log to a buffer so we can assert the detail was logged.
	var buf bytes.Buffer
	orig := log.Default().Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	// Close the store under the server's feet to force a DB error.
	_ = srv.store.Close()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/today", nil)
	r.Host = "127.0.0.1:7777"
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "internal error" {
		t.Errorf("expected generic error, got %q", body["error"])
	}
	// SQLite error text should be in the log, not the response.
	if strings.Contains(w.Body.String(), "sql") || strings.Contains(w.Body.String(), "database") {
		t.Errorf("response leaked DB internals: %s", w.Body.String())
	}
	if !strings.Contains(buf.String(), "today") {
		t.Errorf("server log missing the error context: %s", buf.String())
	}
}

// TestPhantomSessionsExcludedFromAggregates seeds one phantom (start==end,
// zero counts) and one real session in the same project, then asserts
// the phantom doesn't inflate session counts on any aggregate endpoint
// unless include_phantoms=1 is set.
func TestPhantomSessionsExcludedFromAggregates(t *testing.T) {
	srv := newServer(t)
	now := time.Now().UTC().Unix()
	// Phantom: ingester wrote start==stop with no prompts/tools.
	seedSession(t, srv.store, "shp_phantom", "ghost", "", "claude-code", now-500, now-500, 0)
	// Real: nonzero duration, ship attached.
	seedSession(t, srv.store, "shp_real", "ghost", "", "claude-code", now-1000, now-400, 1)

	t.Run("today omits phantom by default", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/today", nil))
		var resp TodayResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Sessions) != 1 || resp.Sessions[0].ID != "shp_real" {
			t.Fatalf("expected only shp_real, got %+v", resp.Sessions)
		}
	})

	t.Run("today includes phantom with opt-in", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/today?include_phantoms=1", nil))
		var resp TodayResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Sessions) != 2 {
			t.Errorf("expected 2 sessions with include_phantoms=1, got %d", len(resp.Sessions))
		}
	})

	t.Run("distribution counts only real session", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/distribution", nil))
		var resp DistributionResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		var got *DistributionProject
		for i, p := range resp.Projects {
			if p.Name == "ghost" {
				got = &resp.Projects[i]
				break
			}
		}
		if got == nil || got.Sessions != 1 {
			t.Errorf("ghost should show 1 session, got %+v", got)
		}
	})

	t.Run("provider-mix counts only real session", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/provider-mix", nil))
		var resp ProviderMixResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if len(resp.Providers) != 1 || resp.Providers[0].Sessions != 1 {
			t.Errorf("expected single provider with 1 session, got %+v", resp.Providers)
		}
	})

	t.Run("agent-skill counts only real session", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/agent-skill-roi", nil))
		var resp AgentSkillResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		// Both seeded sessions have empty agent → roll up under "(none)".
		// Phantom-excluded means "(none)" should have 1 session, not 2.
		for _, r := range resp.ByAgent {
			if r.Name == "(none)" && r.Sessions != 1 {
				t.Errorf("(none) agent should have 1 session, got %d", r.Sessions)
			}
		}
	})

	t.Run("replan-heatmap counts only real session", func(t *testing.T) {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/replan-heatmap", nil))
		var resp ReplanHeatmapResponse
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		var total int
		for _, c := range resp.Cells {
			if c.Project == "ghost" {
				total += c.SessionCount
			}
		}
		if total != 1 {
			t.Errorf("ghost project across all hours should be 1 session, got %d", total)
		}
	})
}

func TestFallbackPageWhenBundleAbsent(t *testing.T) {
	srv := newServer(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	if !contains(w.Body.String(), "dashboard bundle missing") {
		t.Errorf("expected fallback page, got: %s", w.Body.String())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
