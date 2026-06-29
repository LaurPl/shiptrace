package ingest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

var ingestBase = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func fixedClock(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

func readEnd(t *testing.T, s *store.Store, id string) (end sql.NullInt64, inferred bool) {
	t.Helper()
	var inf int
	if err := s.DB().QueryRow(
		`SELECT end_ts, end_ts_inferred FROM sessions WHERE id = ?`, id,
	).Scan(&end, &inf); err != nil {
		t.Fatalf("readEnd %s: %v", id, err)
	}
	return end, inf == 1
}

// rebuildInto opens a fresh store + ingester over the SAME eventsDir with a
// fresh checkpoint — i.e. exactly what `rm shiptrace.db && shiptrace ingest
// --once` does. The clock is pinned so the result is comparable to a live run.
func rebuildInto(t *testing.T, eventsDir string, clock func() time.Time) *store.Store {
	t.Helper()
	home := t.TempDir()
	s, err := store.Open(filepath.Join(home, "shiptrace.db"))
	if err != nil {
		t.Fatalf("rebuild store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ing := New(s, eventsDir, filepath.Join(home, ".ingest-checkpoint.json"))
	ing.SetClock(clock)
	if err := ing.IngestOnce(context.Background()); err != nil {
		t.Fatalf("rebuild IngestOnce: %v", err)
	}
	return s
}

func TestIngestSweepFinalizesAbandonedSession(t *testing.T) {
	f := newFixture(t)
	last := ingestBase.Add(5 * time.Minute)
	f.append(events.Event{EventType: events.SessionStart, Ts: ingestBase, SessionID: "shp_aband", Provider: "claude-code", Label: "x"})
	f.append(events.Event{EventType: events.ToolUse, Ts: last, SessionID: "shp_aband", Tool: "Edit"})
	// No session_stop — the session was killed / window closed.

	f.ingester.SetClock(fixedClock(ingestBase.Add(7 * time.Hour)))
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("IngestOnce: %v", err)
	}

	end, inferred := readEnd(t, f.store, "shp_aband")
	if !end.Valid || end.Int64 != last.Unix() {
		t.Errorf("end_ts = %+v, want %d (last activity)", end, last.Unix())
	}
	if !inferred {
		t.Errorf("end_ts_inferred should be 1")
	}
}

func TestIngestSweepLeavesFreshSessionRunning(t *testing.T) {
	f := newFixture(t)
	f.append(events.Event{EventType: events.SessionStart, Ts: ingestBase, SessionID: "shp_live", Provider: "claude-code", Label: "x"})
	f.append(events.Event{EventType: events.ToolUse, Ts: ingestBase.Add(5 * time.Minute), SessionID: "shp_live", Tool: "Edit"})

	f.ingester.SetClock(fixedClock(ingestBase.Add(1 * time.Hour))) // within window
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("IngestOnce: %v", err)
	}

	if end, _ := readEnd(t, f.store, "shp_live"); end.Valid {
		t.Errorf("fresh session should stay running, end_ts = %+v", end)
	}
}

// The whole point of the marker design: a live ingest that runs the sweep and
// an `ingest --rebuild` over the same JSONL at the same clock converge to
// byte-identical session state.
func TestIngestSweepReplayDeterminism(t *testing.T) {
	f := newFixture(t)
	last := ingestBase.Add(12 * time.Minute)
	f.append(events.Event{EventType: events.SessionStart, Ts: ingestBase, SessionID: "shp_rep", Provider: "claude-code", Label: "x"})
	f.append(events.Event{EventType: events.ToolUse, Ts: last, SessionID: "shp_rep", Tool: "Edit"})
	f.append(events.Event{EventType: events.ReplanSignal, Ts: ingestBase.Add(3 * time.Minute), SessionID: "shp_rep", Metadata: map[string]any{"kind": "pivot_phrase", "weight": 1.0}})

	clock := fixedClock(ingestBase.Add(9 * time.Hour))

	// Live pass.
	f.ingester.SetClock(clock)
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("live IngestOnce: %v", err)
	}
	liveEnd, liveInf := readEnd(t, f.store, "shp_rep")
	var liveScore float64
	if err := f.store.DB().QueryRow(`SELECT replan_score FROM sessions WHERE id = ?`, "shp_rep").Scan(&liveScore); err != nil {
		t.Fatalf("live score: %v", err)
	}

	// Rebuild from scratch over the same JSONL.
	s2 := rebuildInto(t, f.eventsDir, clock)
	rebEnd, rebInf := readEnd(t, s2, "shp_rep")
	var rebScore float64
	if err := s2.DB().QueryRow(`SELECT replan_score FROM sessions WHERE id = ?`, "shp_rep").Scan(&rebScore); err != nil {
		t.Fatalf("rebuild score: %v", err)
	}

	if liveEnd != rebEnd || liveInf != rebInf || liveScore != rebScore {
		t.Errorf("live vs rebuild diverged: end(%v vs %v) inferred(%v vs %v) score(%v vs %v)",
			liveEnd, rebEnd, liveInf, rebInf, liveScore, rebScore)
	}
	if !liveEnd.Valid || liveEnd.Int64 != last.Unix() || !liveInf {
		t.Errorf("expected both finalized at %d/inferred, got end=%+v inferred=%v", last.Unix(), liveEnd, liveInf)
	}
}

// Corner A convergence: live sees the sweep finalize first, then a late real
// stop override it across two passes; rebuild sees the real stop in one pass.
// Both must land on the real stop's timestamp with the marker cleared.
func TestIngestCornerAConvergence(t *testing.T) {
	f := newFixture(t)
	f.append(events.Event{EventType: events.SessionStart, Ts: ingestBase, SessionID: "shp_cA", Provider: "claude-code", Label: "x"})
	f.append(events.Event{EventType: events.ToolUse, Ts: ingestBase.Add(5 * time.Minute), SessionID: "shp_cA", Tool: "Edit"})

	// Live pass 1: sweep finalizes (idle > 6h).
	f.ingester.SetClock(fixedClock(ingestBase.Add(7 * time.Hour)))
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("live pass 1: %v", err)
	}
	if _, inf := readEnd(t, f.store, "shp_cA"); !inf {
		t.Fatalf("precondition: expected inferred end after pass 1")
	}

	// The user finally /quits cleanly — a real session_stop lands.
	realEnd := ingestBase.Add(8 * time.Hour)
	f.append(events.Event{EventType: events.SessionStop, Ts: realEnd, SessionID: "shp_cA"})

	// Live pass 2: real stop overrides the inferred end.
	finalClock := fixedClock(ingestBase.Add(8*time.Hour + time.Minute))
	f.ingester.SetClock(finalClock)
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("live pass 2: %v", err)
	}
	liveEnd, liveInf := readEnd(t, f.store, "shp_cA")

	// Rebuild: single pass over all three events at the same final clock.
	s2 := rebuildInto(t, f.eventsDir, finalClock)
	rebEnd, rebInf := readEnd(t, s2, "shp_cA")

	if liveEnd != rebEnd || liveInf != rebInf {
		t.Errorf("Corner A diverged: live end=%+v inf=%v, rebuild end=%+v inf=%v", liveEnd, liveInf, rebEnd, rebInf)
	}
	if !liveEnd.Valid || liveEnd.Int64 != realEnd.Unix() || liveInf {
		t.Errorf("expected real-stop end %d / inferred=false, got end=%+v inferred=%v", realEnd.Unix(), liveEnd, liveInf)
	}
}
