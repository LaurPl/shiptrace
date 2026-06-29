-- end_ts_inferred marks sessions whose end_ts was inferred by the ingester's
-- staleness sweep (internal/store/sweep.go) rather than set by a real
-- session_stop event. 1 = inferred by the sweep; 0 = from a real session_stop,
-- or end_ts is still NULL.
--
-- Why this column exists. PR #16 moved session_stop emission from the per-turn
-- Stop hook to the once-only SessionEnd hook. SessionEnd does not fire on
-- SIGKILL, terminal-window close, SSH disconnect, OS shutdown, or a CC crash,
-- so an abandoned session never gets a session_stop: end_ts stays NULL forever
-- (shows "running" on Today) and replan_score is never computed. The sweep
-- finalizes such sessions at their last observed activity. SQLite does not keep
-- session_stop events as rows (UpdateSessionStop sets end_ts and discards the
-- event), so without this marker the sweep cannot tell its own inferred end_ts
-- apart from a real-stop end_ts -- and would either clobber real stops or be
-- unable to re-touch its own rows.
--
-- The marker keeps the sweep replay-deterministic. It is a from-scratch
-- recompute that only ever touches rows it owns (end_ts IS NULL OR
-- end_ts_inferred = 1) and never a real-stop row. A late real session_stop can
-- still overwrite an inferred end_ts because UpdateSessionStop guards on
-- `end_ts IS NULL OR end_ts_inferred = 1`; a second real stop (old pre-#16 logs
-- emitted one per turn) finds end_ts set with inferred = 0 and is correctly
-- blocked, preserving first-real-stop-wins.
--
-- Determinism note: the sweep result is a pure function of the JSONL-derived
-- child rows AND the evaluation clock `now`. Given the same JSONL and the same
-- `now`, live ingest and `ingest --rebuild` converge to identical state. The
-- only divergence window is a rebuild executed *during* an idle gap before a
-- resume is written to the log -- inherent to any time-thresholded materialized
-- view, not a bug.
--
-- Additive, forward-only: ALTER ADD COLUMN with a constant default backfills
-- every existing row to 0 without a table rewrite. Existing real-stop rows are
-- already correctly 0; existing open rows are 0 and the sweep will set them to
-- 1 only once they go stale.
ALTER TABLE sessions ADD COLUMN end_ts_inferred INTEGER NOT NULL DEFAULT 0;

-- Partial index over exactly the sweep's candidate set (open or previously
-- inferred sessions). The sweep runs at the end of every ingest pass; this
-- keeps its candidate scan O(candidates) rather than O(all sessions) as the
-- table grows. Footprint is tiny because cleanly-stopped sessions -- the vast
-- majority -- are excluded from the index by the partial predicate.
CREATE INDEX IF NOT EXISTS idx_sessions_sweep_candidates
  ON sessions(start_ts)
  WHERE end_ts IS NULL OR end_ts_inferred = 1;
