package filesystem

import (
	"context"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

// When the staleness sweep finalizes an abandoned session, it stamps
// end_ts = last activity. That moves the row from the "still active"
// (end_ts IS NULL, matched on start_ts) branch of time-window attribution to
// the "ended within window" (end_ts BETWEEN) branch. For a session whose last
// activity — not its start — falls inside the file's window, that is strictly
// more correct: the abandoned session is credited by when it was actually
// working. The method stays time_window; no new attribution enum value.
func TestAttributeTimeWindowUsesSweptEndTs(t *testing.T) {
	s := newScanStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// Session started at base; last activity at base+50m; then abandoned.
	if err := s.UpsertSessionStart(ctx, events.Event{
		EventType: events.SessionStart, Ts: base, SessionID: "shp_sw", Provider: "claude-code", Label: "x",
	}.WithDefaults()); err != nil {
		t.Fatalf("seed start: %v", err)
	}
	if err := s.InsertToolEvent(ctx, events.Event{
		EventType: events.ToolUse, Ts: base.Add(50 * time.Minute), SessionID: "shp_sw", Tool: "Edit",
		FilesTouched: []string{mustAbs(t, "/work/other.md")}, // different file → no file_overlap
	}.WithDefaults()); err != nil {
		t.Fatalf("seed tool: %v", err)
	}

	landed := mustAbs(t, "/work/landed.md")
	mtime := base.Add(60 * time.Minute) // window [base+30m, base+60m] at DefaultTimeWindow=30m

	// Before the sweep the session is still open and only its start_ts (base) is
	// checked against the window — which it is outside — so nothing claims it.
	pre, err := AttributeFile(ctx, s, landed, mtime)
	if err != nil {
		t.Fatalf("pre attribute: %v", err)
	}
	if pre.Method != MethodNone {
		t.Fatalf("precondition: expected None before sweep, got %+v", pre)
	}

	// Sweep finalizes end_ts at last activity (base+50m), which IS in the window.
	if _, err := s.SweepStaleSessions(ctx, base.Add(7*time.Hour), store.DefaultStaleAfter); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	post, err := AttributeFile(ctx, s, landed, mtime)
	if err != nil {
		t.Fatalf("post attribute: %v", err)
	}
	if post.Method != MethodTimeWindow || post.SessionID != "shp_sw" {
		t.Errorf("after sweep expected time_window/shp_sw, got %+v", post)
	}
}
