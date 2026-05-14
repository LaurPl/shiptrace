package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

func TestInsertShipEventStoresAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// First, the parent session — exercise the typical attributed-ship flow.
	if err := s.UpsertSessionStart(ctx, events.Event{
		EventType: events.SessionStart,
		Ts:        time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SessionID: "shp_parent", Provider: "manual", Label: "x",
	}.WithDefaults()); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	e := events.Event{
		EventType: events.Ship,
		Ts:        time.Date(2026, 5, 14, 10, 1, 0, 0, time.UTC),
		SessionID: "shp_parent",
		Metadata: map[string]any{
			"kind":               "manual",
			"ref":                "first ship",
			"attribution_method": "explicit",
			"description":        "first ship",
		},
	}.WithDefaults()
	if err := s.InsertShipEvent(ctx, e); err != nil {
		t.Fatalf("InsertShipEvent: %v", err)
	}

	var (
		sessionID  sql.NullString
		ts         int64
		kind       string
		ref        sql.NullString
		attribMeth sql.NullString
		metaBlob   sql.NullString
	)
	row := s.db.QueryRowContext(ctx, `
		SELECT session_id, ts, kind, ref, attribution_method, metadata
		FROM ship_events ORDER BY id DESC LIMIT 1
	`)
	if err := row.Scan(&sessionID, &ts, &kind, &ref, &attribMeth, &metaBlob); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if sessionID.String != "shp_parent" {
		t.Errorf("session_id: %q", sessionID.String)
	}
	if kind != "manual" {
		t.Errorf("kind: %q", kind)
	}
	if ref.String != "first ship" {
		t.Errorf("ref: %q", ref.String)
	}
	if attribMeth.String != "explicit" {
		t.Errorf("attribution_method: %q", attribMeth.String)
	}
	if !metaBlob.Valid {
		t.Errorf("metadata should retain non-promoted keys (description)")
	}
}

func TestInsertShipEventUnattributed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e := events.Event{
		EventType: events.Ship,
		Ts:        time.Now().UTC(),
		Metadata:  map[string]any{"kind": "manual"},
	}.WithDefaults()
	if err := s.InsertShipEvent(ctx, e); err != nil {
		t.Fatalf("InsertShipEvent: %v", err)
	}
	var sessionID sql.NullString
	if err := s.db.QueryRow(`SELECT session_id FROM ship_events`).Scan(&sessionID); err != nil {
		t.Fatalf("query: %v", err)
	}
	if sessionID.Valid {
		t.Errorf("expected NULL session_id for unattributed ship, got %q", sessionID.String)
	}
}
