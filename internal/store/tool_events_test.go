package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

func seedSession(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.UpsertSessionStart(context.Background(), events.Event{
		EventType: events.SessionStart,
		Ts:        time.Now().UTC(),
		SessionID: id,
		Provider:  "claude-code",
		Label:     "test",
	}.WithDefaults()); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestInsertToolEventStoresFieldsAndBumpsCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "shp_cc1")

	for i := 0; i < 3; i++ {
		err := s.InsertToolEvent(ctx, events.Event{
			EventType:     events.ToolUse,
			Ts:            time.Now().UTC(),
			SessionID:     "shp_cc1",
			Tool:          "Edit",
			ToolInputHash: "sha256:abcdef",
			FilesTouched:  []string{"a.go", "b.go"},
		}.WithDefaults())
		if err != nil {
			t.Fatalf("InsertToolEvent: %v", err)
		}
	}

	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM tool_events WHERE session_id = ?`, "shp_cc1").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 3 {
		t.Errorf("tool_events count: got %d want 3", n)
	}

	var bumped int
	if err := s.db.QueryRow(`SELECT tool_call_count FROM sessions WHERE id = ?`, "shp_cc1").Scan(&bumped); err != nil {
		t.Fatalf("count: %v", err)
	}
	if bumped != 3 {
		t.Errorf("tool_call_count: %d want 3", bumped)
	}

	var files sql.NullString
	if err := s.db.QueryRow(`SELECT files_touched FROM tool_events LIMIT 1`).Scan(&files); err != nil {
		t.Fatalf("files: %v", err)
	}
	if !files.Valid {
		t.Errorf("files_touched should be set")
	}
}

func TestInsertReplanSignalStoresKindAndWeight(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "shp_cc2")

	err := s.InsertReplanSignal(ctx, events.Event{
		EventType: events.ReplanSignal,
		Ts:        time.Now().UTC(),
		SessionID: "shp_cc2",
		Metadata: map[string]any{
			"kind":   "pivot_phrase",
			"phrase": "actually",
			"weight": 1.0,
		},
	}.WithDefaults())
	if err != nil {
		t.Fatalf("InsertReplanSignal: %v", err)
	}

	var kind string
	var weight float64
	var meta sql.NullString
	if err := s.db.QueryRow(`SELECT kind, weight, metadata FROM replan_signals`).Scan(&kind, &weight, &meta); err != nil {
		t.Fatalf("query: %v", err)
	}
	if kind != "pivot_phrase" || weight != 1.0 {
		t.Errorf("kind=%q weight=%v", kind, weight)
	}
	if !meta.Valid {
		t.Errorf("metadata should retain phrase")
	}
}

func TestBumpSessionPromptCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "shp_cc3")

	for i := 0; i < 5; i++ {
		if err := s.BumpSessionPromptCount(ctx, "shp_cc3"); err != nil {
			t.Fatalf("Bump: %v", err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT prompt_count FROM sessions WHERE id = ?`, "shp_cc3").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 5 {
		t.Errorf("prompt_count: %d want 5", n)
	}
}

func TestBumpSessionPromptCountEmptyIDIsNoOp(t *testing.T) {
	s := newTestStore(t)
	if err := s.BumpSessionPromptCount(context.Background(), ""); err != nil {
		t.Fatalf("empty id: %v", err)
	}
}
