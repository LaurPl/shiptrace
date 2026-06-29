package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DefaultStaleAfter is how long a session may sit with no new activity and no
// session_stop before the ingester finalizes it. Long enough that a developer
// stepping away for a meal or a long meeting doesn't have an in-progress
// session yanked out from under them; short enough that a genuinely abandoned
// session (SIGKILL, window close, OS shutdown) doesn't show "running" for days.
const DefaultStaleAfter = 6 * time.Hour

// StaleSweepResult reports what a SweepStaleSessions pass changed. Finalized
// lists sessions newly (or re-) given an inferred end_ts; Reopened lists
// previously-inferred sessions whose activity resumed inside the window, so
// their inferred end_ts was cleared and they are "running" again.
type StaleSweepResult struct {
	Finalized []string
	Reopened  []string
}

// Changed reports whether the sweep mutated any rows. Callers use it to decide
// whether a log line is worth emitting.
func (r StaleSweepResult) Changed() bool {
	return len(r.Finalized) > 0 || len(r.Reopened) > 0
}

// SweepStaleSessions finalizes sessions that were abandoned without a
// session_stop event. It runs at the end of every ingest pass.
//
// A session is "stale" when its last observed activity is older than
// staleAfter relative to now. Last activity is the greatest of start_ts and
// the max ts across the session's tool_events and replan_signals. Ship events
// are deliberately excluded: a ship can be attributed to a session long after
// the fact (file_overlap / time_window), and a post-hoc ship must not revive a
// dead session's clock.
//
// The finalized end_ts is the last-activity timestamp, NOT now — duration must
// stay truthful (a session abandoned at 14:00 and swept at 23:00 ended at
// 14:00, not 23:00) and the row must not land in the "recently ended"
// time-window-attribution band with a fabricated end time.
//
// The pass is a from-scratch recompute, a pure function of the JSONL-derived
// rows plus now, so a live ingest and an `ingest --rebuild` converge:
//   - open (end_ts NULL) and stale            → finalize at last activity, inferred=1
//   - inferred and stale, activity advanced   → re-finalize at the new last activity
//   - inferred and no longer stale (resumed)  → clear end_ts, inferred=0, score 0
//
// It only ever touches rows it owns (end_ts IS NULL OR end_ts_inferred = 1) and
// never a real-stop row, so a real session_stop — whenever it lands — is
// authoritative. All UPDATEs and the per-session replan recomputes run in a
// single transaction so a partial failure can never leave a finalized session
// unscored (which the marker would make permanent until a rebuild).
//
// staleAfter <= 0 disables the sweep and returns an empty result.
func (s *Store) SweepStaleSessions(ctx context.Context, now time.Time, staleAfter time.Duration) (StaleSweepResult, error) {
	var res StaleSweepResult
	if staleAfter <= 0 {
		return res, nil
	}
	cutoff := now.Add(-staleAfter).Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("store: sweep begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op once Commit succeeds

	cands, err := loadSweepCandidates(ctx, tx)
	if err != nil {
		return res, err
	}

	for _, c := range cands {
		stale := c.lastActivity < cutoff
		switch {
		case !c.endTs.Valid && stale:
			// Open and abandoned → finalize at last activity.
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET end_ts = ?, end_ts_inferred = 1 WHERE id = ?`,
				c.lastActivity, c.id,
			); err != nil {
				return res, fmt.Errorf("store: sweep finalize %s: %w", c.id, err)
			}
			if _, err := computeAndStoreReplanScore(ctx, tx, c.id); err != nil {
				return res, fmt.Errorf("store: sweep score %s: %w", c.id, err)
			}
			res.Finalized = append(res.Finalized, c.id)

		case c.inferred && stale && c.endTs.Int64 != c.lastActivity:
			// Previously swept, then more activity landed (events ingested out
			// of order, or a slow tail) → re-finalize at the new last activity
			// and rescore, so the result still matches a rebuild.
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET end_ts = ? WHERE id = ? AND end_ts_inferred = 1`,
				c.lastActivity, c.id,
			); err != nil {
				return res, fmt.Errorf("store: sweep refinalize %s: %w", c.id, err)
			}
			if _, err := computeAndStoreReplanScore(ctx, tx, c.id); err != nil {
				return res, fmt.Errorf("store: sweep rescore %s: %w", c.id, err)
			}
			res.Finalized = append(res.Finalized, c.id)

		case c.inferred && !stale:
			// Previously swept but activity resumed inside the window — the
			// session is alive again. Clear the inferred end so it reads as
			// running, and reset the score (open sessions score 0 by
			// convention; it is recomputed if/when it is finalized again).
			if _, err := tx.ExecContext(ctx,
				`UPDATE sessions SET end_ts = NULL, end_ts_inferred = 0, replan_score = 0 WHERE id = ? AND end_ts_inferred = 1`,
				c.id,
			); err != nil {
				return res, fmt.Errorf("store: sweep reopen %s: %w", c.id, err)
			}
			res.Reopened = append(res.Reopened, c.id)
		}
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("store: sweep commit: %w", err)
	}
	return res, nil
}

// sweepCandidate is one row the sweep may act on: a session that is open or was
// previously finalized by the sweep, plus its derived last-activity timestamp.
type sweepCandidate struct {
	id           string
	lastActivity int64
	endTs        sql.NullInt64
	inferred     bool
}

// loadSweepCandidates returns the open-or-inferred sessions with their derived
// last-activity ts. The rows are read fully into a slice before the caller
// issues any UPDATE so we never write under an open cursor on the same tx.
//
// The WHERE matches the idx_sessions_sweep_candidates partial index exactly, so
// this scans only the candidate set, not every session. Real-stop rows (end_ts
// set, inferred = 0) are excluded — the sweep must never touch them.
func loadSweepCandidates(ctx context.Context, q dbtx) ([]sweepCandidate, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT
			s.id,
			MAX(
				s.start_ts,
				COALESCE((SELECT MAX(ts) FROM tool_events    WHERE session_id = s.id), 0),
				COALESCE((SELECT MAX(ts) FROM replan_signals WHERE session_id = s.id), 0)
			) AS last_activity,
			s.end_ts,
			s.end_ts_inferred
		FROM sessions s
		WHERE s.end_ts IS NULL OR s.end_ts_inferred = 1
	`)
	if err != nil {
		return nil, fmt.Errorf("store: sweep scan: %w", err)
	}
	defer rows.Close()

	var out []sweepCandidate
	for rows.Next() {
		var (
			c           sweepCandidate
			inferredInt int
		)
		if err := rows.Scan(&c.id, &c.lastActivity, &c.endTs, &inferredInt); err != nil {
			return nil, fmt.Errorf("store: sweep scan row: %w", err)
		}
		c.inferred = inferredInt == 1
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: sweep scan rows: %w", err)
	}
	return out, nil
}
