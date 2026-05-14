# shiptrace — Build Plan

Concrete sequence for getting from empty repo to dogfoodable v0.1. Roughly a week of focused effort, less with agent assistance.

## Pre-build (do once, before any code)

1. **Confirm name availability** for `shiptrace`:
   - `whois shiptrace.dev` / `.app` / `.ai`
   - USPTO TESS: search "shiptrace" across all classes — https://tmsearch.uspto.gov/search/search-information
   - EUIPO eSearch: same — https://euipo.europa.eu/eSearch/
   - Confirm no npm/PyPI/crates.io collision: `npm view shiptrace`, https://pypi.org/project/shiptrace/, https://crates.io/crates/shiptrace
2. **Create empty GitHub repo** (public), `github.com/<user>/shiptrace`.
3. **Add license**: copy MIT license text verbatim from https://choosealicense.com/licenses/mit/. Set copyright line to `Copyright (c) 2026 <full name>`.
4. **Add `.gitignore`** for Go + Node.
5. **Set up GPG or SSH commit signing.** Every commit should show "Verified" on GitHub. This is the attribution anchor.
6. **Tag `v0.0.0`** on the empty repo so there's a public starting point.
7. **Reserve `shiptrace.dev` domain** (Cloudflare Registrar, Porkbun, or Namecheap).

## Build sequence (7 days, adjustable)

### Day 1 — Manual recorder + events store

The counterintuitive but correct starting point. It exercises the full event pipeline without any provider integration.

- Scaffold Go project: `cmd/shiptrace/`, `internal/events/`, `internal/store/`.
- Implement `shiptrace session start "..."` and `shiptrace session stop`.
- Implement `shiptrace ship "..."`.
- Implement JSONL append-only event log at `~/.shiptrace/events/YYYY-MM-DD.jsonl`.
- Implement SQLite schema:
  ```sql
  CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    project TEXT,
    start_ts INTEGER NOT NULL,
    end_ts INTEGER,
    model TEXT,
    agent TEXT,
    skill TEXT,
    prompt_count INTEGER DEFAULT 0,
    tool_call_count INTEGER DEFAULT 0,
    tokens_in INTEGER DEFAULT 0,
    tokens_out INTEGER DEFAULT 0,
    replan_score REAL DEFAULT 0
  );

  CREATE TABLE tool_events (
    session_id TEXT NOT NULL,
    ts INTEGER NOT NULL,
    tool TEXT NOT NULL,
    tool_input_hash TEXT,
    files_touched TEXT,  -- JSON array
    FOREIGN KEY (session_id) REFERENCES sessions(id)
  );

  CREATE TABLE ship_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT,  -- nullable; unattributed ships allowed
    ts INTEGER NOT NULL,
    kind TEXT NOT NULL,  -- commit | pr_merged | publish | red_to_green | manual | ...
    ref TEXT,  -- commit SHA, PR URL, etc.
    magnitude TEXT,  -- JSON: {loc: 42, files: 3} or {failures_resolved: 12}
    metadata TEXT,  -- JSON
    attribution_method TEXT  -- explicit | file_overlap | time_window
  );

  CREATE INDEX idx_tool_events_session ON tool_events(session_id, ts);
  CREATE INDEX idx_ship_events_session ON ship_events(session_id, ts);
  CREATE INDEX idx_sessions_project_time ON sessions(project, start_ts);
  ```
- Implement ingester: cron-style flush of JSONL → SQLite every N minutes (or on `Stop` events).
- Smoke test: `shiptrace session start "test"`, run for a minute, `shiptrace ship "first ship"`, `shiptrace session stop`. Verify row counts in SQLite.

**Milestone**: end of day, the manual flow works end-to-end.

### Day 2 — Claude Code recorder

- Implement a small Go binary `shiptrace-cc-hook` that reads JSON from stdin, extracts session_id and event fields, writes to the events log.
- Subcommands or arguments for each hook type: `session-start`, `prompt`, `tool-use`, `subagent-stop`, `stop`.
- Latency budget: <30ms per invocation. Profile with `time` on a hot path.
- Write `~/.claude/settings.json` snippet:
  ```json
  {
    "hooks": {
      "SessionStart": [{"hooks": [{"type": "command", "command": "shiptrace-cc-hook session-start"}]}],
      "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "shiptrace-cc-hook prompt"}]}],
      "PostToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "shiptrace-cc-hook tool-use"}]}],
      "SubagentStop": [{"hooks": [{"type": "command", "command": "shiptrace-cc-hook subagent-stop"}]}],
      "Stop": [{"hooks": [{"type": "command", "command": "shiptrace-cc-hook stop"}]}]
    }
  }
  ```
- Implement `shiptrace init` to append to (not replace) the user's existing `~/.claude/settings.json`.
- Implement `shiptrace doctor` to verify hooks are wired and latency is acceptable.
- Smoke test: run a real Claude Code session, verify events appear in the log.

**Milestone**: end of day, Claude Code sessions are captured automatically.

### Day 3 — Git ship adapter

- Implement `shiptrace-git-postcommit`: reads the active session ID from `.shiptrace/.current-session` (written by SessionStart hook), runs `git rev-parse HEAD` and `git diff-tree`, emits a `ship` event with kind=`commit`.
- Write the post-commit hook installer: `shiptrace adapter install git` adds the hook to `.git/hooks/post-commit` in the target repo.
- Handle the stale-session-pointer footgun: expire `.current-session` after N minutes of inactivity.
- Add a `shiptrace ship` PR-merged variant that polls `gh pr list --state merged` periodically and emits `ship` events with kind=`pr_merged` for matches.

**Milestone**: end of day, commits and PR merges in instrumented repos are joined to their originating sessions.

### Day 4 — Replan detection + filesystem adapter

- **Replan detection**: post-hoc transcript parser. Walks `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`, finds TodoWrite payloads, detects status reversals and task deletions, counts pivot phrases in subsequent prompts. Emits `replan_signal` events. Aggregate into `replan_score` per session at session_stop time.
- **Filesystem adapter**: watches user-configured `ship_paths` (glob patterns from config). When a file appears or is modified in a ship path, attribute to the most recent session that touched that file (via tool_events join), emit a `ship` event with kind=`file_landed`.

**Milestone**: end of day, replan score is computed; non-git work flows (writing, design) can be tracked.

### Day 5 — Dashboard MVP

- Local HTTP server (Go) serving a static React bundle on `localhost:7777`.
- Five views, no more:
  1. **Today** — Gantt-style timeline of sessions, color-coded.
  2. **Distribution** — sessions-to-ship histogram per project.
  3. **Replan heatmap** — project × hour-of-day.
  4. **Agent/skill ROI** — bar chart of sessions-to-ship grouped by agent/skill.
  5. **Provider mix** — placeholder for when multi-provider data exists.
- Recharts for charting. Vite + React for the bundle. Tailwind for layout if desired but plain CSS is fine.
- Implement `shiptrace report --week` CLI summary as a parallel surface (some users will never open the dashboard).

**Milestone**: end of day, `shiptrace serve` gives a useful view of the last week's data.

### Day 6 — Packaging and docs

- Package the binary set: `shiptrace`, `shiptrace-cc-hook`, `shiptrace-git-postcommit`, dashboard server, all bundled.
- Cross-compile for darwin-arm64, darwin-amd64, linux-amd64, windows-amd64.
- Write installer script `install.sh` that downloads the right binary, places it on PATH, optionally runs `shiptrace init`.
- Write README. Lead with the insight from running it on yourself, not with the feature list (see `docs/README-draft.md` if you spin one up).
- Write `docs/privacy.md` explicitly stating what's captured and what isn't.
- Write `docs/adapters.md` explaining how to write a custom adapter.

**Milestone**: end of day, someone else could install shiptrace and use it.

### Day 7 — Dogfood and prep launch

- Run shiptrace on shiptrace itself. Generate at least 24 hours of session-to-ship data.
- Capture screenshots from the dashboard with real data.
- Write the launch blog post. Lead with a specific surprising finding from your own data ("my Tuesday afternoons are replan hell"). Don't lead with the architecture.
- Don't promote yet. Let the post sit; let the data accumulate; consider waiting 2-3 weeks of personal use before public launch.

**Milestone**: end of day, v0.1.0 tagged, README polished, screenshots in hand, launch post drafted but unpublished.

## Decisions to make explicitly (write these down once, don't revisit)

| Decision | Choice | Why |
|---|---|---|
| License | MIT | Maximum adoption, minimum friction |
| Hook language | Go | Compiled, <30ms cold start, single-file distribution |
| Dashboard language | React + Recharts via Vite | Standard, easy hiring (if ever needed) |
| Storage | SQLite + JSONL | Single-file, durable, no ops |
| Sync | Local-only by default | Privacy is a feature, not an afterthought |
| Provider priority | Manual → Claude Code → Codex → Cursor → others | Manual exercises full pipeline; CC is your daily driver |
| Commit signing | GPG or SSH (your pick) | Attribution anchor |

## Risk register

- **Hook latency drifts above 30ms** as features grow → benchmark on every PR, fail CI if regression.
- **Session boundaries don't align across providers** → document the inconsistency, don't paper over it.
- **Manual recorder gets buried under fancier features** → keep it visible in docs; non-coders will use it most.
- **"Exploration mode" projects look like failures** → first-class config flag from day one.
- **Attribution mis-fires** → surface attribution chain in dashboard; allow user corrections.
- **Scope creep toward "AI productivity coaching"** → re-read the non-goals section before adding any feature that uses an LLM.

## Don't build (yet)

- Cloud sync.
- Team/multi-user dashboards.
- LLM-summarized insights.
- Mobile app.
- Slack/Discord notifications.
- Anything called "AI-powered" inside shiptrace itself.

These aren't bad ideas; they're just not v0.1. Some will never be in shiptrace at all.
