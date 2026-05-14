package store

import (
	"context"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

func TestComputeAndStoreReplanScore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "shp_rs1")

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	// Seed two pivot phrases + a TodoWrite reversal pair.
	for _, ev := range []events.Event{
		{EventType: events.ReplanSignal, Ts: now, SessionID: "shp_rs1", Metadata: map[string]any{"kind": "pivot_phrase", "weight": 1.0, "phrase": "actually"}},
		{EventType: events.ReplanSignal, Ts: now.Add(time.Minute), SessionID: "shp_rs1", Metadata: map[string]any{"kind": "todowrite", "weight": 0.5, "pending": 2, "total": 2}},
		{EventType: events.ReplanSignal, Ts: now.Add(2 * time.Minute), SessionID: "shp_rs1", Metadata: map[string]any{"kind": "todowrite", "weight": 0.5, "pending": 0, "in_progress": 1, "completed": 1, "total": 2}},
		// reversal:
		{EventType: events.ReplanSignal, Ts: now.Add(3 * time.Minute), SessionID: "shp_rs1", Metadata: map[string]any{"kind": "todowrite", "weight": 0.5, "pending": 2, "total": 2}},
		{EventType: events.ReplanSignal, Ts: now.Add(4 * time.Minute), SessionID: "shp_rs1", Metadata: map[string]any{"kind": "pivot_phrase", "weight": 1.0, "phrase": "scrap that"}},
	} {
		if err := s.InsertReplanSignal(ctx, ev.WithDefaults()); err != nil {
			t.Fatalf("InsertReplanSignal: %v", err)
		}
	}

	score, err := s.ComputeAndStoreReplanScore(ctx, "shp_rs1")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if score <= 0 || score >= 1 {
		t.Errorf("score out of (0,1): %v", score)
	}

	var stored float64
	if err := s.db.QueryRow(`SELECT replan_score FROM sessions WHERE id = ?`, "shp_rs1").Scan(&stored); err != nil {
		t.Fatalf("query: %v", err)
	}
	if stored < score-1e-9 || stored > score+1e-9 {
		t.Errorf("stored %v != computed %v", stored, score)
	}
}

func TestComputeAndStoreReplanScoreEmptySession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedSession(t, s, "shp_clean")
	score, err := s.ComputeAndStoreReplanScore(ctx, "shp_clean")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if score != 0 {
		t.Errorf("expected 0 score for a session with no signals, got %v", score)
	}
}

func TestComputeAndStoreReplanScoreEmptyIDNoop(t *testing.T) {
	s := newTestStore(t)
	score, err := s.ComputeAndStoreReplanScore(context.Background(), "")
	if err != nil || score != 0 {
		t.Errorf("empty id should be no-op, got score=%v err=%v", score, err)
	}
}
