package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUpsertSessionStartInsertsRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	e := events.Event{
		EventType: events.SessionStart,
		Ts:        time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SessionID: "shp_abc",
		Provider:  "manual",
		Project:   "shiptrace",
		Label:     "writing slides",
		Metadata:  map[string]any{"provider_session_id": "cc_xyz"},
	}.WithDefaults()

	if err := s.UpsertSessionStart(ctx, e); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.GetSession(ctx, "shp_abc")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != "shp_abc" {
		t.Errorf("ID: %q", got.ID)
	}
	if !got.Label.Valid || got.Label.String != "writing slides" {
		t.Errorf("Label: %+v", got.Label)
	}
	if got.Provider != "manual" {
		t.Errorf("Provider: %q", got.Provider)
	}
	if !got.ProviderSessionID.Valid || got.ProviderSessionID.String != "cc_xyz" {
		t.Errorf("ProviderSessionID: %+v", got.ProviderSessionID)
	}
	if got.StartTs != e.Ts.Unix() {
		t.Errorf("StartTs: got %d want %d", got.StartTs, e.Ts.Unix())
	}
	if got.EndTs.Valid {
		t.Errorf("EndTs should be NULL before stop")
	}
}

func TestUpsertSessionStartIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e := events.Event{
		EventType: events.SessionStart, Ts: time.Now().UTC(),
		SessionID: "shp_dup", Provider: "manual", Label: "first",
	}.WithDefaults()
	if err := s.UpsertSessionStart(ctx, e); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Same id, different label — should be ignored.
	e2 := e
	e2.Label = "second"
	if err := s.UpsertSessionStart(ctx, e2); err != nil {
		t.Fatalf("second: %v", err)
	}
	got, err := s.GetSession(ctx, "shp_dup")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Label.String != "first" {
		t.Errorf("idempotent upsert overwrote label: %q", got.Label.String)
	}
}

func TestUpdateSessionStopSetsEndTs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	start := events.Event{
		EventType: events.SessionStart, Ts: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SessionID: "shp_stop", Provider: "manual", Label: "x",
	}.WithDefaults()
	if err := s.UpsertSessionStart(ctx, start); err != nil {
		t.Fatalf("start: %v", err)
	}
	stop := events.Event{
		EventType: events.SessionStop, Ts: start.Ts.Add(30 * time.Second),
		SessionID: "shp_stop",
	}.WithDefaults()
	if err := s.UpdateSessionStop(ctx, stop); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got, _ := s.GetSession(ctx, "shp_stop")
	if !got.EndTs.Valid || got.EndTs.Int64 != stop.Ts.Unix() {
		t.Errorf("EndTs: %+v want %d", got.EndTs, stop.Ts.Unix())
	}
}

func TestUpdateSessionStopBackfillsMissingRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	stop := events.Event{
		EventType: events.SessionStop, Ts: time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC),
		SessionID: "shp_orphan", Provider: "manual",
	}.WithDefaults()
	if err := s.UpdateSessionStop(ctx, stop); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got, err := s.GetSession(ctx, "shp_orphan")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.StartTs != stop.Ts.Unix() || !got.EndTs.Valid {
		t.Errorf("backfill row mismatch: start=%d end=%+v", got.StartTs, got.EndTs)
	}
}

func TestGetSessionMissingReturnsErrNoRows(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSession(context.Background(), "shp_never")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}
