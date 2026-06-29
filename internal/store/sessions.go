package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/LaurPl/shiptrace/internal/events"
)

// Session is the row shape returned by GetSession. Nullable columns use the
// sql package's Null types so callers can distinguish unset from zero.
type Session struct {
	ID                string
	Label             sql.NullString
	Provider          string
	ProviderSessionID sql.NullString
	Project           sql.NullString
	StartTs           int64
	EndTs             sql.NullInt64
	Model             sql.NullString
	Agent             sql.NullString
	Skill             sql.NullString
}

// UpsertSessionStart writes a session_start event into the sessions table.
// Idempotent — replaying the same event_id is a no-op so the ingester can
// safely re-run after a crash.
func (s *Store) UpsertSessionStart(ctx context.Context, e events.Event) error {
	if e.SessionID == "" {
		return fmt.Errorf("store: session_start missing session_id")
	}
	provSessionID := extractString(e.Metadata, "provider_session_id")
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, label, provider, provider_session_id, project, start_ts, model, agent, skill)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`,
		e.SessionID,
		nullableString(e.Label),
		coalesceString(e.Provider, "unknown"),
		nullableString(provSessionID),
		nullableString(e.Project),
		e.Ts.Unix(),
		nullableString(e.Model),
		nullableString(e.Agent),
		nullableString(e.Skill),
	)
	if err != nil {
		return fmt.Errorf("store: upsert session_start: %w", err)
	}
	return nil
}

// UpdateSessionStop sets end_ts from a real session_stop event. If the session
// row doesn't exist (events out of order), we insert a minimal row so the join
// targets aren't lost.
//
// The guard `end_ts IS NULL OR end_ts_inferred = 1` makes a real stop
// authoritative over the staleness sweep: it claims a still-open session and
// also overwrites an end_ts the sweep inferred (flipping the marker back to 0),
// so a session that was finalized-by-staleness and then cleanly /quit ends up
// with its true end time. It does NOT overwrite an end_ts already set by an
// earlier real stop (end_ts set, inferred = 0): old pre-#16 logs emitted a
// session_stop every turn, and first-real-stop-wins must hold on replay.
func (s *Store) UpdateSessionStop(ctx context.Context, e events.Event) error {
	if e.SessionID == "" {
		return fmt.Errorf("store: session_stop missing session_id")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET end_ts = ?, end_ts_inferred = 0 WHERE id = ? AND (end_ts IS NULL OR end_ts_inferred = 1)`,
		e.Ts.Unix(), e.SessionID,
	)
	if err != nil {
		return fmt.Errorf("store: update session_stop: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		// Either the session was never started (out-of-order events) or it was
		// already stopped by a real stop. The first case is the one we can
		// heal: insert a minimal row so analytics don't lose the session.
		// end_ts_inferred is named explicitly as 0 so this real-stop-derived
		// end is never mistaken for a sweep-inferred one (and so the row's
		// phantom classification can't silently shift if the column default
		// ever changes).
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO sessions (id, provider, start_ts, end_ts, end_ts_inferred)
			VALUES (?, ?, ?, ?, 0)
			ON CONFLICT(id) DO NOTHING
		`,
			e.SessionID,
			coalesceString(e.Provider, "unknown"),
			e.Ts.Unix(),
			e.Ts.Unix(),
		)
		if err != nil {
			return fmt.Errorf("store: backfill stopped session: %w", err)
		}
	}
	return nil
}

// GetSession returns the row for sessionID, or (nil, sql.ErrNoRows) if absent.
func (s *Store) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, label, provider, provider_session_id, project, start_ts, end_ts, model, agent, skill
		FROM sessions WHERE id = ?
	`, sessionID)
	var out Session
	if err := row.Scan(
		&out.ID, &out.Label, &out.Provider, &out.ProviderSessionID, &out.Project,
		&out.StartTs, &out.EndTs, &out.Model, &out.Agent, &out.Skill,
	); err != nil {
		return nil, err
	}
	return &out, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func coalesceString(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func extractString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
