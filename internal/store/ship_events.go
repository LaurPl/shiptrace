package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LaurPl/shiptrace/internal/events"
)

// InsertShipEvent appends a row to ship_events. Day 1 has no idempotency key:
// the eventlog Reader's checkpoint guarantees we don't re-ingest the same
// line, so duplicates would only arise from manual rebuilds — acceptable
// trade-off at v0.1 scale.
func (s *Store) InsertShipEvent(ctx context.Context, e events.Event) error {
	kind := extractString(e.Metadata, "kind")
	if kind == "" {
		kind = "manual"
	}
	ref := extractString(e.Metadata, "ref")
	attrMethod := extractString(e.Metadata, "attribution_method")

	// Stash the description and any other metadata as JSON for later analysis.
	// Stripping the keys we promoted to dedicated columns avoids double-storing them.
	stash := cloneMetadata(e.Metadata)
	delete(stash, "kind")
	delete(stash, "ref")
	delete(stash, "attribution_method")
	metaJSON, err := json.Marshal(stash)
	if err != nil {
		return fmt.Errorf("store: marshal ship metadata: %w", err)
	}
	if string(metaJSON) == "null" || string(metaJSON) == "{}" {
		metaJSON = nil
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO ship_events (session_id, ts, kind, ref, magnitude, metadata, attribution_method)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		nullableString(e.SessionID),
		e.Ts.Unix(),
		kind,
		nullableString(ref),
		nil, // magnitude — day 3+ (git diff stats, etc.)
		nullableBytes(metaJSON),
		nullableString(attrMethod),
	)
	if err != nil {
		return fmt.Errorf("store: insert ship_event: %w", err)
	}
	return nil
}

// ShipEventRow is the read shape returned by ListShipEvents. metadata
// stays as raw JSON bytes; the server-side handler can pass it through
// as a json.RawMessage so the client gets the same shape that landed in
// the JSONL line.
type ShipEventRow struct {
	SessionID         string
	Ts                int64
	Kind              string
	Ref               string
	Metadata          []byte
	AttributionMethod string
}

// ListShipEvents returns every ship_event attributed to the session, in
// ts order. Includes rows where session_id IS NULL only if the caller
// passed sessionID == "" — drill-down callers should always pass an id.
func (s *Store) ListShipEvents(ctx context.Context, sessionID string) ([]ShipEventRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(session_id, ''), ts, kind, COALESCE(ref, ''),
		       COALESCE(metadata, ''), COALESCE(attribution_method, '')
		FROM ship_events
		WHERE session_id = ?
		ORDER BY ts ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: query ship_events: %w", err)
	}
	defer rows.Close()
	out := make([]ShipEventRow, 0)
	for rows.Next() {
		var r ShipEventRow
		var meta string
		if err := rows.Scan(&r.SessionID, &r.Ts, &r.Kind, &r.Ref, &meta, &r.AttributionMethod); err != nil {
			return nil, fmt.Errorf("store: scan ship_event: %w", err)
		}
		if meta != "" {
			r.Metadata = []byte(meta)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func cloneMetadata(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
