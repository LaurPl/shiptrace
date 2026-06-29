package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

// sweepBase is a fixed wall-clock anchor so every sweep test is deterministic.
var sweepBase = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// seedSessionAt inserts a session_start with an explicit start_ts.
func seedSessionAt(t *testing.T, s *Store, id string, startTs time.Time) {
	t.Helper()
	if err := s.UpsertSessionStart(context.Background(), events.Event{
		EventType: events.SessionStart,
		Ts:        startTs,
		SessionID: id,
		Provider:  "claude-code",
		Label:     "test",
	}.WithDefaults()); err != nil {
		t.Fatalf("seedSessionAt %s: %v", id, err)
	}
}

func toolAt(t *testing.T, s *Store, id string, ts time.Time) {
	t.Helper()
	if err := s.InsertToolEvent(context.Background(), events.Event{
		EventType: events.ToolUse, Ts: ts, SessionID: id, Tool: "Edit",
	}.WithDefaults()); err != nil {
		t.Fatalf("toolAt %s: %v", id, err)
	}
}

func replanAt(t *testing.T, s *Store, id, kind string, weight float64, ts time.Time, extra map[string]any) {
	t.Helper()
	meta := map[string]any{"kind": kind, "weight": weight}
	for k, v := range extra {
		meta[k] = v
	}
	if err := s.InsertReplanSignal(context.Background(), events.Event{
		EventType: events.ReplanSignal, Ts: ts, SessionID: id, Metadata: meta,
	}.WithDefaults()); err != nil {
		t.Fatalf("replanAt %s: %v", id, err)
	}
}

// readSession returns the sweep-relevant columns directly.
func readSession(t *testing.T, s *Store, id string) (end sql.NullInt64, inferred bool, score float64) {
	t.Helper()
	var inf int
	if err := s.db.QueryRow(
		`SELECT end_ts, end_ts_inferred, replan_score FROM sessions WHERE id = ?`, id,
	).Scan(&end, &inf, &score); err != nil {
		t.Fatalf("readSession %s: %v", id, err)
	}
	return end, inf == 1, score
}

func mustSweep(t *testing.T, s *Store, now time.Time) StaleSweepResult {
	t.Helper()
	res, err := s.SweepStaleSessions(context.Background(), now, DefaultStaleAfter)
	if err != nil {
		t.Fatalf("SweepStaleSessions: %v", err)
	}
	return res
}

func TestSweepFinalizesAbandonedSession(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_ab", sweepBase)
	last := sweepBase.Add(5 * time.Minute)
	toolAt(t, s, "shp_ab", last)

	res := mustSweep(t, s, sweepBase.Add(7*time.Hour))

	if len(res.Finalized) != 1 || res.Finalized[0] != "shp_ab" {
		t.Fatalf("Finalized = %v, want [shp_ab]", res.Finalized)
	}
	end, inferred, _ := readSession(t, s, "shp_ab")
	if !end.Valid || end.Int64 != last.Unix() {
		t.Errorf("end_ts = %+v, want %d (last activity)", end, last.Unix())
	}
	if !inferred {
		t.Errorf("end_ts_inferred should be 1 after sweep")
	}
}

func TestSweepSkipsFreshSession(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_fresh", sweepBase)
	toolAt(t, s, "shp_fresh", sweepBase.Add(5*time.Minute))

	// Only one hour has passed — well within the 6h window.
	res := mustSweep(t, s, sweepBase.Add(1*time.Hour))

	if res.Changed() {
		t.Fatalf("expected no change, got %+v", res)
	}
	end, _, _ := readSession(t, s, "shp_fresh")
	if end.Valid {
		t.Errorf("fresh session should stay open, end_ts = %+v", end)
	}
}

// BLOCKING-1 from review: the inferred end_ts must be the last-activity ts,
// never now(). A session abandoned at 14:00 and swept at 23:00 ended at 14:00.
func TestSweepUsesLastActivityNotNow(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_la", sweepBase)
	last := sweepBase.Add(2 * time.Hour)
	toolAt(t, s, "shp_la", last)

	now := sweepBase.Add(10 * time.Hour)
	mustSweep(t, s, now)

	end, _, _ := readSession(t, s, "shp_la")
	if !end.Valid || end.Int64 != last.Unix() {
		t.Fatalf("end_ts = %+v, want %d (last activity, NOT now=%d)", end, last.Unix(), now.Unix())
	}
}

// Ship events must not count as activity: a ship can be attributed to a session
// long after the fact and must not revive a dead session's clock.
func TestSweepExcludesShipEventsFromActivity(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_ship", sweepBase)
	toolLast := sweepBase.Add(1 * time.Minute)
	toolAt(t, s, "shp_ship", toolLast)
	// A recent ship (5h in) would make the session look alive if it counted.
	if _, err := s.db.Exec(
		`INSERT INTO ship_events (session_id, ts, kind) VALUES (?, ?, ?)`,
		"shp_ship", sweepBase.Add(5*time.Hour).Unix(), "manual",
	); err != nil {
		t.Fatalf("seed ship: %v", err)
	}

	mustSweep(t, s, sweepBase.Add(7*time.Hour))

	end, inferred, _ := readSession(t, s, "shp_ship")
	if !end.Valid || end.Int64 != toolLast.Unix() {
		t.Fatalf("end_ts = %+v, want %d (tool ts; ship must be ignored)", end, toolLast.Unix())
	}
	if !inferred {
		t.Errorf("should be finalized despite the recent ship")
	}
}

func TestSweepIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_idem", sweepBase)
	toolAt(t, s, "shp_idem", sweepBase.Add(5*time.Minute))
	now := sweepBase.Add(7 * time.Hour)

	first := mustSweep(t, s, now)
	end1, inf1, score1 := readSession(t, s, "shp_idem")

	second := mustSweep(t, s, now)
	end2, inf2, score2 := readSession(t, s, "shp_idem")

	if len(first.Finalized) != 1 {
		t.Fatalf("first pass should finalize 1, got %v", first.Finalized)
	}
	if second.Changed() {
		t.Fatalf("second pass should be a no-op, got %+v", second)
	}
	if end1 != end2 || inf1 != inf2 || score1 != score2 {
		t.Errorf("state drifted across idempotent sweeps: (%v,%v,%v) vs (%v,%v,%v)",
			end1, inf1, score1, end2, inf2, score2)
	}
}

func TestSweepNeverTouchesRealStop(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSessionAt(t, s, "shp_real", sweepBase)
	toolAt(t, s, "shp_real", sweepBase.Add(5*time.Minute))
	realEnd := sweepBase.Add(10 * time.Minute)
	if err := s.UpdateSessionStop(ctx, events.Event{
		EventType: events.SessionStop, Ts: realEnd, SessionID: "shp_real",
	}.WithDefaults()); err != nil {
		t.Fatalf("UpdateSessionStop: %v", err)
	}

	res := mustSweep(t, s, sweepBase.Add(7*time.Hour))

	if res.Changed() {
		t.Fatalf("sweep must not touch a real-stop row, got %+v", res)
	}
	end, inferred, _ := readSession(t, s, "shp_real")
	if !end.Valid || end.Int64 != realEnd.Unix() || inferred {
		t.Errorf("real stop got clobbered: end=%+v inferred=%v", end, inferred)
	}
}

// Corner A: a real session_stop that lands AFTER the sweep finalized the
// session (CC idle >6h, then clean /quit) must win and flip the marker back.
func TestRealStopOverridesInferredEnd(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSessionAt(t, s, "shp_cornerA", sweepBase)
	toolAt(t, s, "shp_cornerA", sweepBase.Add(5*time.Minute))

	mustSweep(t, s, sweepBase.Add(7*time.Hour))
	if _, inferred, _ := readSession(t, s, "shp_cornerA"); !inferred {
		t.Fatalf("precondition: expected inferred end after sweep")
	}

	realEnd := sweepBase.Add(8 * time.Hour)
	if err := s.UpdateSessionStop(ctx, events.Event{
		EventType: events.SessionStop, Ts: realEnd, SessionID: "shp_cornerA",
	}.WithDefaults()); err != nil {
		t.Fatalf("late real stop: %v", err)
	}

	end, inferred, _ := readSession(t, s, "shp_cornerA")
	if !end.Valid || end.Int64 != realEnd.Unix() {
		t.Errorf("real stop should win: end_ts = %+v, want %d", end, realEnd.Unix())
	}
	if inferred {
		t.Errorf("marker should flip to 0 once a real stop claims the session")
	}
}

// Old pre-#16 logs emitted a session_stop per turn. The second real stop must
// not move end_ts — first-real-stop-wins, even with the relaxed guard.
func TestSecondRealStopIsBlocked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSessionAt(t, s, "shp_two", sweepBase)
	first := sweepBase.Add(10 * time.Minute)
	second := sweepBase.Add(20 * time.Minute)
	for _, ts := range []time.Time{first, second} {
		if err := s.UpdateSessionStop(ctx, events.Event{
			EventType: events.SessionStop, Ts: ts, SessionID: "shp_two",
		}.WithDefaults()); err != nil {
			t.Fatalf("stop @ %v: %v", ts, err)
		}
	}
	end, inferred, _ := readSession(t, s, "shp_two")
	if !end.Valid || end.Int64 != first.Unix() || inferred {
		t.Errorf("first stop should win: end=%+v inferred=%v want %d/false", end, inferred, first.Unix())
	}
}

// Corner B: a swept session that resumes within the window is reopened, so it
// reads as running again and its score resets (open sessions score 0).
func TestSweepReopensResumedSession(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_resume", sweepBase)
	toolAt(t, s, "shp_resume", sweepBase.Add(5*time.Minute))
	replanAt(t, s, "shp_resume", "pivot_phrase", 1.0, sweepBase.Add(4*time.Minute), nil)

	// First sweep finalizes it with a real (>0) score.
	mustSweep(t, s, sweepBase.Add(7*time.Hour))
	if _, inferred, score := readSession(t, s, "shp_resume"); !inferred || score <= 0 {
		t.Fatalf("precondition: inferred=%v score=%v, want inferred & score>0", inferred, score)
	}

	// New activity lands, then a sweep runs while it's still recent.
	resumeTs := sweepBase.Add(7*time.Hour + 10*time.Minute)
	toolAt(t, s, "shp_resume", resumeTs)
	res := mustSweep(t, s, sweepBase.Add(7*time.Hour+20*time.Minute))

	if len(res.Reopened) != 1 || res.Reopened[0] != "shp_resume" {
		t.Fatalf("Reopened = %v, want [shp_resume]", res.Reopened)
	}
	end, inferred, score := readSession(t, s, "shp_resume")
	if end.Valid {
		t.Errorf("reopened session should read as running, end_ts = %+v", end)
	}
	if inferred {
		t.Errorf("marker should be cleared on reopen")
	}
	if score != 0 {
		t.Errorf("score should reset to 0 on reopen, got %v", score)
	}
}

// A previously-swept session whose activity advanced (a late-arriving event)
// is re-finalized at the new last activity, still stale, so rebuild matches.
func TestSweepRefinalizesOnAdvancedActivity(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_adv", sweepBase)
	toolAt(t, s, "shp_adv", sweepBase.Add(5*time.Minute))

	mustSweep(t, s, sweepBase.Add(7*time.Hour))
	if end, _, _ := readSession(t, s, "shp_adv"); end.Int64 != sweepBase.Add(5*time.Minute).Unix() {
		t.Fatalf("precondition end_ts wrong: %+v", end)
	}

	// A late tool_use arrives (still old relative to the next, later sweep).
	advanced := sweepBase.Add(10 * time.Minute)
	toolAt(t, s, "shp_adv", advanced)
	res := mustSweep(t, s, sweepBase.Add(8*time.Hour))

	if len(res.Finalized) != 1 {
		t.Fatalf("expected re-finalize, got %+v", res)
	}
	end, inferred, _ := readSession(t, s, "shp_adv")
	if !end.Valid || end.Int64 != advanced.Unix() || !inferred {
		t.Errorf("end_ts = %+v inferred=%v, want %d/true", end, inferred, advanced.Unix())
	}
}

func TestSweepComputesReplanScore(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_score", sweepBase)
	// A pivot + a TodoWrite reversal pair so the score is unambiguously > 0.
	replanAt(t, s, "shp_score", "pivot_phrase", 1.0, sweepBase.Add(1*time.Minute), nil)
	replanAt(t, s, "shp_score", "todowrite", 0.5, sweepBase.Add(2*time.Minute), map[string]any{"pending": 0, "completed": 2, "total": 2})
	replanAt(t, s, "shp_score", "todowrite", 0.5, sweepBase.Add(3*time.Minute), map[string]any{"pending": 2, "total": 2})

	mustSweep(t, s, sweepBase.Add(7*time.Hour))

	_, inferred, score := readSession(t, s, "shp_score")
	if !inferred {
		t.Fatalf("expected finalized session")
	}
	if score <= 0 {
		t.Errorf("replan_score should be computed > 0 at sweep time, got %v", score)
	}
}

func TestSweepDisabledWhenThresholdNonPositive(t *testing.T) {
	s := newTestStore(t)
	seedSessionAt(t, s, "shp_off", sweepBase)
	toolAt(t, s, "shp_off", sweepBase.Add(5*time.Minute))

	res, err := s.SweepStaleSessions(context.Background(), sweepBase.Add(100*time.Hour), 0)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Changed() {
		t.Fatalf("staleAfter=0 must disable the sweep, got %+v", res)
	}
	if end, _, _ := readSession(t, s, "shp_off"); end.Valid {
		t.Errorf("disabled sweep must not finalize, end_ts = %+v", end)
	}
}

// A swept EMPTY session (real start, no work, abandoned) gets end_ts==start_ts
// and zero counts — structurally like a phantom, but it had a real
// session_start so it must NOT be filtered. A genuine phantom (start-less real
// stop, inferred=0) still is. This is the Q2 phantom-filter interaction.
func TestSweptEmptySessionSurvivesPhantomFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Swept empty session: real start, nothing else, abandoned.
	seedSessionAt(t, s, "shp_empty", sweepBase)
	mustSweep(t, s, sweepBase.Add(7*time.Hour))
	if end, inferred, _ := readSession(t, s, "shp_empty"); !end.Valid || end.Int64 != sweepBase.Unix() || !inferred {
		t.Fatalf("precondition: empty swept session should be end_ts==start_ts inferred=1, got end=%+v inferred=%v", end, inferred)
	}

	// Genuine phantom: a real session_stop with no preceding start.
	if err := s.UpdateSessionStop(ctx, events.Event{
		EventType: events.SessionStop, Ts: sweepBase, SessionID: "shp_phantom", Provider: "manual",
	}.WithDefaults()); err != nil {
		t.Fatalf("phantom stop: %v", err)
	}

	survivors := phantomFilteredIDs(t, s)
	if !survivors["shp_empty"] {
		t.Errorf("swept empty session must survive the phantom filter")
	}
	if survivors["shp_phantom"] {
		t.Errorf("genuine phantom must be filtered out")
	}
}

// phantomFilteredIDs returns the set of session ids that survive PhantomFilterSQL.
func phantomFilteredIDs(t *testing.T, s *Store) map[string]bool {
	t.Helper()
	rows, err := s.db.Query(`SELECT s.id FROM sessions s WHERE 1=1` + PhantomFilterSQL)
	if err != nil {
		t.Fatalf("phantom query: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[id] = true
	}
	return out
}
