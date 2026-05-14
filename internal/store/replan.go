package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LaurPl/shiptrace/internal/replan"
)

// ComputeAndStoreReplanScore loads every replan_signal for sessionID,
// computes the score, and writes it back to sessions.replan_score.
// Intended to be called once per session at session_stop ingestion time.
//
// Returns the computed score so callers (and tests) can assert on it.
func (s *Store) ComputeAndStoreReplanScore(ctx context.Context, sessionID string) (float64, error) {
	if sessionID == "" {
		return 0, nil
	}
	signals, err := s.loadReplanSignals(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	reversals := replan.DetectReversals(signals)
	score := replan.ComputeScore(signals, reversals)

	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET replan_score = ? WHERE id = ?`,
		score, sessionID,
	); err != nil {
		return 0, fmt.Errorf("store: update replan_score: %w", err)
	}
	return score, nil
}

// loadReplanSignals reads the per-session replan_signal rows in ascending
// ts order and re-hydrates the per-kind metadata back onto the Signal
// struct so the replan package can work on a clean shape.
func (s *Store) loadReplanSignals(ctx context.Context, sessionID string) ([]replan.Signal, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts, kind, weight, metadata
		FROM replan_signals
		WHERE session_id = ?
		ORDER BY ts ASC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: query replan_signals: %w", err)
	}
	defer rows.Close()

	var out []replan.Signal
	for rows.Next() {
		var (
			ts     int64
			kind   string
			weight float64
			meta   sql.NullString
		)
		if err := rows.Scan(&ts, &kind, &weight, &meta); err != nil {
			return nil, fmt.Errorf("store: scan replan_signal: %w", err)
		}
		sig := replan.Signal{
			Ts:     time.Unix(ts, 0).UTC(),
			Kind:   kind,
			Weight: weight,
		}
		if meta.Valid && strings.TrimSpace(meta.String) != "" {
			var m map[string]any
			if err := json.Unmarshal([]byte(meta.String), &m); err == nil {
				sig.Pending, _ = intField(m, "pending")
				sig.InProgress, _ = intField(m, "in_progress")
				sig.Completed, _ = intField(m, "completed")
				sig.Total, _ = intField(m, "total")
			}
		}
		out = append(out, sig)
	}
	return out, rows.Err()
}

// intField pulls an integer out of a generic JSON-decoded map; JSON numbers
// land as float64 in Go's standard decoder.
func intField(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	}
	return 0, false
}
