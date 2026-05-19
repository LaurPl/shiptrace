-- Backfill: rewrite obvious worktree-style basenames in sessions.project
-- to NULL so they no longer surface as distinct "projects" in the
-- dashboard. The CC default worktree naming is <word>-<word>-<digits>
-- (e.g. "jovial-rubin-687768"); we can't recover the parent project from
-- the basename alone, so the row drops into "(unassigned)" until it ages
-- out of the reporting window.
--
-- Going forward, the recorder normalizes worktree paths to the parent
-- project at session_start (see internal/project.Normalize), so new rows
-- never land here.
--
-- Idempotent: subsequent runs find no matching rows.
UPDATE sessions
SET project = NULL
WHERE project IS NOT NULL
  AND project GLOB '[a-z]*-[a-z]*-[0-9][0-9][0-9][0-9][0-9][0-9]*';
