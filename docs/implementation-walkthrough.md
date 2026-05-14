# shiptrace — implementation walkthrough

A guided tour of every file in shiptrace, what job it does, why it exists, and how the pieces fit. Written for someone learning the codebase from the outside, including the Go/JS conventions that aren't obvious unless you've spent time in those ecosystems.

If you're impatient, the next 200 words plus the diagram give you the whole picture.

---

## The 30-second picture

shiptrace is three programs that share one storage area:

```
┌──────────────────────┐        EVENT CAPTURE        ┌─────────────────────────────┐
│ shiptrace            │  CLI commands write here    │                             │
│   session start      │ ─────────────────────────▶  │                             │
│   ship               │                             │  ~/.shiptrace/events/       │
│   session stop       │                             │  YYYY-MM-DD.jsonl           │
└──────────────────────┘                             │                             │
                                                     │  append-only newline-       │
┌──────────────────────┐                             │  delimited JSON.            │
│ shiptrace-cc-hook    │  Claude Code calls this 5x  │                             │
│   session-start      │  per session, writes events │  THIS FILE IS THE TRUTH.    │
│   prompt             │ ─────────────────────────▶  │  Everything else is         │
│   tool-use           │                             │  derived from it.           │
│   subagent-stop      │                             │                             │
│   stop               │                             └─────────────┬───────────────┘
└──────────────────────┘                                           │
                                                                   │ fsnotify tail
┌──────────────────────┐                                           │ + per-file
│ shiptrace-git-       │  .git/hooks/post-commit calls this        │ byte-offset
│   postcommit         │  ─────────────────────────────────────▶   │ checkpoint
└──────────────────────┘                                           ▼
                                                     ┌─────────────────────────────┐
                                                     │  ~/.shiptrace/shiptrace.db  │
                                                     │  SQLite, materialized view  │
                                                     │  of the JSONL log.          │
                                                     │                             │
                                                     │  Tables:                    │
                                                     │   sessions, ship_events,    │
                                                     │   tool_events,              │
                                                     │   replan_signals            │
                                                     └─────────────┬───────────────┘
                                                                   │
                                                                   │ read-only
                                                                   ▼
                                              ┌─────────────────────────────────────┐
                                              │  QUERY SURFACES                     │
                                              │                                     │
                                              │  shiptrace report --week            │
                                              │      (plaintext)                    │
                                              │                                     │
                                              │  shiptrace serve  →  localhost:7777 │
                                              │      (Go HTTP + embedded React)     │
                                              │      5 views: today, distribution,  │
                                              │      replan heatmap, agent/skill,   │
                                              │      provider mix                   │
                                              └─────────────────────────────────────┘
```

**One invariant carries the whole design**: the JSONL log is the source of truth. SQLite is a *materialized view* — if you delete the database, running `shiptrace ingest --once` rebuilds it from the JSONL. Every recorder and adapter writes JSONL only; only the ingester writes to SQLite. That asymmetry is what makes the whole thing recoverable.

---

## The five questions shiptrace answers

shiptrace exists to answer five questions that the existing tooling doesn't:

1. **How many agent sessions does a typical commit / publish / lecture take in this project?** ("sessions-to-ship")
2. **When does my work pivot mid-session?** ("replan score")
3. **Which agents and skills actually pay off?** ("agent / skill ROI")
4. **Which providers (Claude Code, Codex, Cursor) work better for me on comparable work?** ("provider mix")
5. **What's happening today across all my projects?** ("today timeline")

Every code path you'll read below traces back to one of these. When you wonder "why does this file exist?", it almost always points to one of these five.

---

## Four subsystems

The code under `internal/` and `cmd/` falls into four groups:

| Subsystem | What it does | Key packages |
|---|---|---|
| **Event capture** | Programs that *generate* events: the CLI, the Claude Code hook, the git post-commit hook, the filesystem scanner. | `cmd/*`, `internal/cli/`, `internal/eventlog/`, `internal/hooks/claudecode/`, `internal/adapters/{git,filesystem}/` |
| **Storage** | The append-only JSONL log + the SQLite materialized view + the ingester that maps one to the other. | `internal/eventlog/`, `internal/store/`, `internal/ingest/` |
| **Attribution** | Decides which session a given ship event belongs to (so a `git commit` attributes to a CC session, etc). | `internal/attrib/`, `internal/session/` |
| **Query / UI** | The HTTP server, JSON endpoints, React dashboard, and CLI reports. | `internal/server/`, `internal/cli/{serve,report}.go`, `web/` |

The boundary lines between subsystems are how we keep the codebase auditable. The strictest rule: **only `internal/ingest` is allowed to write to SQLite.** Recorders and adapters always write JSONL. If you ever see a recorder importing `internal/store`, that's a regression.

---

## How the internal/ packages depend on each other

The arrows below mean "imports" in Go. The graph is intentionally shallow — `internal/events` is at the bottom and everything depends on it; `internal/store` is at the top and only the ingester writes to it.

```
                  internal/events  ◀── used by everyone (just types + IDs)
                       ▲
       ┌───────────────┼───────────────┐
       │               │               │
internal/eventlog  internal/session  internal/store
       ▲               ▲               ▲
       │               │               │
       └─────────┬─────┘               │
                 │                     │
          internal/attrib              │
                 │                     │
       ┌─────────┴───────────┐         │
       │                     │         │
internal/hooks/claudecode    │   internal/ingest  ◀── ONLY package that
internal/adapters/git        │                       writes to internal/store
internal/adapters/filesystem │
       │                     │
       └─────────┬───────────┘
                 │
            internal/cli   ◀── glues everything for users
                 │
              cmd/*         ◀── three small main.go entrypoints
```

Tests live alongside code (`foo.go` + `foo_test.go`), Go-idiomatic. Run them all with `go test ./...`. Run one package with `go test ./internal/replan/`.

---

## The 7-day build, file by file

Each "day" was a tagged commit milestone (`v0.0.1` through `v0.0.6`, then `v0.1.0`). The day groupings below match those tags.

### Day 1 — Foundation: manual recorder + event store

**The day's goal**: stand up the pipeline end-to-end with the simplest possible recorder, so we can prove "session_start → ship → session_stop → SQLite row" works before adding Claude Code or git anywhere near it.

| File | Purpose | Connects to |
|---|---|---|
| `go.mod`, `go.sum` | Declare the Go module name and lock dependency versions. | The Go toolchain. |
| `LICENSE` | MIT license. | (legal anchor) |
| `.gitignore` | Keep node_modules, binaries, the bundle output, and local data out of git. | git |
| `cmd/shiptrace/main.go` | The main binary's entrypoint. Just calls `cli.Execute()`. | `internal/cli` |
| `internal/events/event.go` | Defines `Event` — the canonical record every recorder produces and every consumer reads. `EventType` constants (`session_start`, `prompt`, `tool_use`, `replan_signal`, `ship`, `session_stop`). `SchemaVersion = "1"`. | Everyone. |
| `internal/events/id.go` | `NewSessionID()` — generates `shp_<12-char-base32>` from `crypto/rand`. 60 bits of entropy. | `cli`, `claudecode`, `dogfood-seed` |
| `internal/paths/paths.go` | Resolves on-disk locations: `~/.shiptrace/`, `~/.shiptrace/events/`, `~/.shiptrace/shiptrace.db`, the pointer files. Honors `$SHIPTRACE_HOME` so tests can redirect everything to a temp dir. | Every file that touches disk. |
| `internal/eventlog/writer.go` | `Writer.Append(e)` — opens `<events-dir>/YYYY-MM-DD.jsonl`, marshals `e` to JSON, writes one line + newline, `fsync`s. Day-rotates automatically based on event timestamp. | Recorders, adapters, dogfood-seed. |
| `internal/eventlog/reader.go` | `ScanFile(path, startOffset, fn)` — streams a JSONL file line by line and reports the *byte offset* after each event. The ingester uses that offset as a resume point. | `internal/ingest` |
| `internal/session/pointer.go` | The `.current-session` file — small JSON pointer with the active session id, label, started-at, last-activity timestamp. Atomic write (write-temp + rename). Used by manual recorder to remember "what session am I in" across CLI calls. | `cli/session`, `cli/ship`, `attrib` |
| `internal/attrib/resolve.go` | Implements the **attribution precedence chain**: `--session` flag > `$SHIPTRACE_SESSION_ID` env > per-project pointer (Day 3) > global pointer > unattributed. Surfaces conflicts so silent miscategorization is impossible. | `cli/ship`, `cli/session` |
| `internal/display/color.go` | ANSI color helper. Honors `NO_COLOR` env var. Off when stdout isn't a TTY. | `display/feedback`, `cli` |
| `internal/display/tty.go` | Stdlib-thin TTY check via `golang.org/x/term`. | `display/color` |
| `internal/display/relative_time.go` | `Relative(t, now)` returns `"3s ago"`, `"2h ago"`, `"5d ago"`. Pure function — fully unit-testable. | `display/feedback` |
| `internal/display/feedback.go` | The **attribution feedback line** — the green `✓` or yellow `⚠` you see after every ship/session command. Critical UX: makes silent miscategorization visible. | `cli/ship`, `cli/session` |
| `internal/store/store.go` | Opens the SQLite database via `modernc.org/sqlite` (pure-Go driver — no CGo, cross-compiles trivially). Sets `journal_mode=WAL`, `synchronous=NORMAL`, `foreign_keys=ON`. Runs migrations on open. | `internal/ingest`, `internal/server` |
| `internal/store/migrations.go` | Reads `migrations/*.sql` via `//go:embed`, applies each migration once, records version in `schema_migrations`. Idempotent — safe to re-run. | `store/store.go` |
| `internal/store/migrations/0001_init.sql` | The four core tables: `sessions`, `tool_events`, `ship_events`, plus indexes. | The DB. |
| `internal/store/sessions.go` | `UpsertSessionStart`, `UpdateSessionStop`, `GetSession`. Handles out-of-order events by back-filling a minimal row if `session_stop` arrives before `session_start`. | `internal/ingest` |
| `internal/store/ship_events.go` | `InsertShipEvent`. Promotes well-known metadata keys (`kind`, `ref`, `attribution_method`) into dedicated columns; stashes the rest as JSON. | `internal/ingest` |
| `internal/store/tool_events.go` | Stub. Filled in Day 2. The file exists from Day 1 so future days don't churn the import graph. | (placeholder) |
| `internal/ingest/checkpoint.go` | `Checkpoints` map: filename → last-ingested byte offset. Atomic save (tmp + rename). Crash-safe resume by construction. | `internal/ingest/ingester` |
| `internal/ingest/ingester.go` | The long-running tail daemon. `Run(ctx)` does an `IngestOnce` catch-up, then uses `fsnotify` to react to file writes. Debounces bursts (50ms). `IngestOnce()` is the cron-friendly one-shot variant. | `eventlog/reader`, `store`, `cli/ingest` |
| `internal/cli/root.go` | Cobra command tree. Wires subcommands. | All other `cli/*.go` |
| `internal/cli/session.go` | `shiptrace session start/stop`. Validates label, generates session id, writes event, writes pointer, prints feedback. | `events`, `eventlog`, `session`, `display` |
| `internal/cli/ship.go` | `shiptrace ship "description"`. Resolves attribution via `attrib`, writes event, prints feedback. | `attrib`, `events`, `eventlog`, `display` |
| `internal/cli/ingest.go` | `shiptrace ingest [--once]`. SIGINT/SIGTERM-aware shutdown for the daemon variant. | `store`, `ingest` |
| `internal/cli/init.go`, `doctor.go` | Stubs. Filled in Day 2. Hidden subcommands so help output is stable. | (placeholders) |
| `internal/config/config.go` | Stub. Filled in Day 4. | (placeholder) |

**What you can run at the end of Day 1**:
```
shiptrace session start "writing slides"
shiptrace ship "first draft"
shiptrace session stop
shiptrace ingest --once
sqlite3 ~/.shiptrace/shiptrace.db "SELECT * FROM sessions"
```
You see the row. The pipeline works.

---

### Day 2 — Claude Code recorder + `init` + `doctor`

**The day's goal**: capture real Claude Code session events without slowing CC down. The hard constraint is a 30ms p99 budget per hook invocation (CC fires hooks on the hot path of every prompt and tool use).

| File | Purpose | Connects to |
|---|---|---|
| `cmd/shiptrace-cc-hook/main.go` | The second binary. **Stdlib-only** — no cobra, no SQLite, no fsnotify — to hit the 30ms cold-start budget. Reads JSON from stdin, dispatches on `os.Args[1]` to one of five handlers. | `internal/hooks/claudecode`, `internal/eventlog`, `internal/paths` |
| `internal/hooks/claudecode/payload.go` | Parses the CC hook JSON. Preserves unknown top-level fields in `Extras` so future CC additions don't require code changes here. | `cc-hook/main.go` |
| `internal/hooks/claudecode/sessionmap.go` | Maps Claude Code's session UUID → shiptrace's `shp_` id. One tiny file per session under `~/.shiptrace/cc-sessions/<uuid>`. O(1) read and write — critical for the hot path. | CC hook handlers |
| `internal/hooks/claudecode/privacy.go` | The two privacy env vars (`SHIPTRACE_LOG_PROMPT_TEXT`, `SHIPTRACE_LOG_TOOL_INPUT`) and the `HashString` / `HashBytes` helpers. Verbatim capture requires explicit opt-in. | CC hook handlers |
| `internal/hooks/claudecode/replan.go` | The pivot-phrase regex and the TodoWrite payload summarizer (`Pending`, `InProgress`, `Completed`, `Total`). | CC hook handlers, Day-4 score |
| `internal/hooks/claudecode/handler.go` | Per-event handlers: `HandleSessionStart`, `HandlePrompt`, `HandleToolUse`, `HandleSubagentStop`, `HandleStop`. Each writes one or two events to the JSONL log and updates the per-project pointer (Day 3 wires this). | `events`, `eventlog`, `session` |
| `internal/hooks/claudecode/json.go` | Tiny helper for best-effort JSON parsing of partially-known shapes (`files_touched` extraction from tool inputs). | handler.go |
| `internal/store/tool_events.go` | Filled in. `InsertToolEvent` + `InsertReplanSignal` + `BumpSessionPromptCount`. Each emits one row and increments a denormalized counter on `sessions` for cheap dashboard queries. | `ingest` |
| `internal/store/migrations/0002_replan_signals.sql` | Adds the `replan_signals` table (kind, weight, metadata JSON). | The DB. |
| `internal/ccsettings/settings.go` | Reads, merges, and writes `~/.claude/settings.json`. The merge **never** replaces existing user hooks — it appends a managed shiptrace block. Idempotent across binary path changes. | `cli/init`, `cli/doctor` |
| `internal/cli/init.go` (filled) | `shiptrace init` — finds the hook binary, computes the new settings.json, prints a before/after diff, asks confirmation (unless `--yes`), writes atomically. | `ccsettings` |
| `internal/cli/doctor.go` (filled) | `shiptrace doctor` — checks home is writable, hook binary on `$PATH`, all 5 hooks installed, and runs a **latency probe** (10 synthetic invocations against a temp `SHIPTRACE_HOME`, reports p50/p99). Catches drift early. | `ccsettings`, `exec.LookPath` |
| `internal/ingest/ingester.go` (modified) | Dispatch expanded: prompt → `BumpSessionPromptCount`, tool_use → `InsertToolEvent`, replan_signal → `InsertReplanSignal`. | `store` |

**The critical design call**: replan signals are captured **live in the hook**, never by parsing CC's transcript file later. CC's per-session transcript JSONL at `~/.claude/projects/<encoded-cwd>/<session-id>.jsonl` is undocumented and changes between releases. By doing pivot-phrase regex and TodoWrite-payload hashing live, we never depend on CC's internal format.

**Measured Day-2 latency**: p50 = 8.7 ms, p99 = 11.0 ms. Budget was 30 ms.

---

### Day 3 — Git ship adapter + PR-merge poller + per-project pointer

**The day's goal**: when you `git commit`, shiptrace records a ship event automatically and attributes it to the active Claude Code session in that repo.

| File | Purpose | Connects to |
|---|---|---|
| `internal/session/pointer.go` (extended) | Added `LastActivity` field + `IsStale(now, maxAge)` + `Touch(path, now)`. CC hook touches the pointer on every event so the post-commit adapter can tell "active session" from "stale 8h ago." | CC hook, git post-commit |
| `internal/session/projectpointer.go` | `ProjectKey(cwd)` resolves the git repo root, hashes it (`sha256`-truncated-to-16-hex), and that hash is the filename under `~/.shiptrace/project-pointers/<hash>.json`. **We never write into the user's repo** — the hash maps a repo path to a per-project pointer without dirtying `git status`. | CC hook, git post-commit, ship resolver |
| `internal/hooks/claudecode/handler.go` (modified) | `HandleSessionStart` writes the per-project pointer. Every event Touches it. `HandleStop` deletes it. | `session` |
| `cmd/shiptrace-git-postcommit/main.go` | The third binary. Stdlib-only. Reads cwd → finds git repo root → reads per-project pointer (or falls back to unattributed) → collects commit metadata → emits ship event. | `adapters/git`, `eventlog`, `paths` |
| `internal/adapters/git/postcommit.go` | `CollectCommitMetadata`: `git rev-parse HEAD`, `git log -1 --format=...`, `git show --name-only`, `git show --shortstat`. Returns SHA, author, subject, files, insertions/deletions. `BuildShipEvent` packages it into the canonical `Event`. `ResolveSession` reads the per-project pointer. | `cmd/shiptrace-git-postcommit` |
| `internal/adapters/git/installer.go` | Writes `.git/hooks/post-commit` with a managed block (`# shiptrace-post-commit (managed) — start ... end`). Idempotent. Preserves any user-authored content above/below the markers. Uninstall removes only the managed block. | `cli/adapter` |
| `internal/adapters/git/prpoll.go` | The PR-merge poller. Shells out to `gh pr list --state merged --json url,title,number,mergedAt,baseRefName --limit N`. Dedupes against a state file (`~/.shiptrace/.pr-merge-state.json`) keyed by PR URL. | `cli/adapter` |
| `internal/cli/adapter.go` | The `shiptrace adapter ...` subcommand tree: `install git`, `uninstall git`, `status`, `pr-poll`. Each is a small cobra command. | `adapters/git`, `paths` |
| `internal/attrib/resolve.go` (extended) | `Resolve(Inputs)` now takes an `Inputs` struct with `FlagValue`, `EnvValue`, `ProjectPointerPath`, `GlobalPointerPath`, `MaxStaleness`. Added the per-project pointer slot to the precedence chain. Skips stale pointers automatically. | `cli/ship`, `cli/session-stop` |

**The deviation from the build plan** worth flagging: the design doc suggested putting the per-project pointer at `<repo>/.shiptrace/.current-session`. We don't — we hash the repo path and store the pointer under `~/.shiptrace/project-pointers/<hash>.json`. Reason: keeps user repos clean (no `.gitignore` additions needed) and makes uninstall a one-line `rm -rf ~/.shiptrace`.

---

### Day 4 — Replan score + filesystem ship adapter + config loader

**The day's goal**: turn the raw `replan_signal` events Day 2 captured into a single 0-1 score per session, and add a ship adapter for non-code work (files landing in a configured publish path).

| File | Purpose | Connects to |
|---|---|---|
| `internal/replan/score.go` | Pure functions, no I/O. `DetectReversals(signals)` walks consecutive TodoWrite snapshots and flags any signal where `pending` grew faster than `total` (i.e., something was moved back to pending — that's the reversal). `ComputeScore(signals, reversals)` is `1 - exp(-w/5)` so the score saturates between 0 and 1. | `store/replan` |
| `internal/store/replan.go` | `ComputeAndStoreReplanScore(ctx, sessionID)` loads all replan signals for a session, calls into the `replan` package, writes the score back to `sessions.replan_score`. | `ingest`, `replan` |
| `internal/ingest/ingester.go` (modified) | On every `session_stop` event, the ingester now also calls `ComputeAndStoreReplanScore`. That's the moment to lock in the score. | `store/replan` |
| `internal/config/config.go` (filled) | YAML loader for `~/.shiptrace/config.yaml`. v0.1 schema: `projects.<name>.{paths, ship_paths, mode}`. We intentionally don't surface adapters/privacy/retention yet — those land in v0.2 when the shape's settled. | `cli/adapter` |
| `internal/adapters/filesystem/attribute.go` | `AttributeFile(ctx, store, path, mtime)` resolves which session a file land belongs to. Precedence: `file_overlap` (JSON1 query joining `tool_events.files_touched` against the path) → `time_window` (the most recent session that ended in the last 30 min) → `none`. | `cli/adapter` |
| `internal/adapters/filesystem/scanner.go` | `Scan(state, shipPaths)` walks the configured ship_paths (glob expansion + recursive directory walk), reports `Match{Path, Mtime}` for any file newer than the last seen mtime. `EmitShipEvents` attributes each and writes a `file_landed` ship event. | `cli/adapter`, `eventlog`, `store` |
| `internal/cli/adapter.go` (extended) | Added `shiptrace adapter scan-fs` — one-shot scan, cron-friendly. Loads config, picks ship_paths (filterable by `--project`), scans, attributes, emits. Dedupe state at `~/.shiptrace/.fs-state.json`. | `config`, `adapters/filesystem` |

**Deviation #1**: the build plan called for a post-hoc transcript parser to detect replans. We deleted that idea on Day 2 (live capture) and Day 4 only computes the score, which is a substantially smaller surface area.

**Deviation #2**: the FS adapter is `scan-fs` (one-shot, cron-friendly), not a `watch-fs` daemon. Same reasoning as `pr-poll`: we already have one long-running process (the ingester); two daemons doubles operational complexity for marginal benefit.

---

### Day 5 — Dashboard MVP

**The day's goal**: a local HTTP server on `localhost:7777` with five views — today's timeline, sessions-to-ship distribution, replan heatmap, agent/skill ROI, provider mix.

| File | Purpose | Connects to |
|---|---|---|
| `internal/server/server.go` | The HTTP server. Embeds the React bundle via `//go:embed` (declared in `cmd/shiptrace/main.go` and threaded through `cli.SetBundle`). When the bundle is missing, serves a helpful fallback HTML so the JSON API is still usable. | `cli/serve`, `store` |
| `internal/server/api.go` | Shared helpers: `writeJSON`, `writeError`, `parseDays`, the version probe. | All `api_*.go` |
| `internal/server/api_today.go` | `GET /api/today` — sessions started in the last 24h with their ship counts and replan scores. | Dashboard "today" view |
| `internal/server/api_distribution.go` | `GET /api/distribution?days=N` — per-project sessions, ships, sessions-per-ship, mean replan. | Dashboard "distribution" view |
| `internal/server/api_replan.go` | `GET /api/replan-heatmap?days=N` — (project, hour-of-day) cells with mean score + session count. Uses SQLite's `strftime('%H', ts, 'unixepoch')` for the hour. | Dashboard "replan" view |
| `internal/server/api_agentskill.go` | `GET /api/agent-skill-roi?days=N` — `by_agent` + `by_skill` rows. | Dashboard "agent/skill" view |
| `internal/server/api_providermix.go` | `GET /api/provider-mix?days=N` — per-provider sessions + ships. | Dashboard "provider mix" view |
| `internal/cli/serve.go` | `shiptrace serve` — cobra wrapper. Opens the store, builds the server with the embedded bundle, runs it with SIGINT/SIGTERM-aware shutdown. | `server`, `paths`, `store` |
| `internal/cli/report.go` | `shiptrace report --day/--week/--month/--days N` — the plaintext parallel of the dashboard. Some users will never open a browser. | `store` |
| `cmd/shiptrace/main.go` (modified) | Adds `//go:embed all:web/dist` and calls `cli.SetBundle(bundleFS)` so the binary carries the dashboard with it. | `internal/cli`, `web/dist` |
| `cmd/shiptrace/web/dist/.placeholder` | One-line file. Exists because `//go:embed` errors out if the target directory is empty. A fresh clone can `go build` before `npm run build`. | `cmd/shiptrace/main.go` |
| `web/package.json` | Node dependencies: React 18, Recharts, react-router, Vite, TypeScript. | The Node toolchain. |
| `web/vite.config.ts` | Vite config. Build output goes to `../cmd/shiptrace/web/dist` so the Go `//go:embed` finds it. Dev mode proxies `/api/` to `127.0.0.1:7777` for hot-reload workflows. | Vite |
| `web/tsconfig.json` | TypeScript compiler config. | tsc |
| `web/index.html` | The HTML shell Vite uses. One `<div id="root">` + the bootstrap script. | Vite |
| `web/src/main.tsx` | React bootstrap: `createRoot(document.getElementById("root")).render(...)`. | `App.tsx` |
| `web/src/styles.css` | Terminal-y aesthetic: dark bg, monospace, green accents, minimal chrome. No Tailwind — we'd add a build-system dependency for ~30 utility classes. | All views |
| `web/src/api.ts` | TypeScript types and `fetch` wrappers for each endpoint. Each function maps 1:1 to a Go handler. | All views |
| `web/src/components/Loader.tsx` | `useLoader` hook + `LoaderBoundary` component. Handles loading / error / empty / data states so each view doesn't repeat them. | All views |
| `web/src/App.tsx` | Top-level shell: header, tab nav (5 tabs), `<Routes>`, status footer reading `/api/version`. | All views, `api.ts` |
| `web/src/views/Today.tsx` | Horizontal session bars on a CSS-positioned 24h timeline. Green = shipped, blue = in-progress, red = unshipped. | `api.ts` |
| `web/src/views/Distribution.tsx` | Recharts horizontal `BarChart` per project + raw-numbers table. | `api.ts`, Recharts |
| `web/src/views/ReplanHeatmap.tsx` | **CSS grid heatmap**, not Recharts. Project rows × 24 hour columns; opacity scales with session count; hue goes warm-cool by score. Recharts has no native heatmap and the homebrew Treemap hacks looked worse than 30 lines of CSS. | `api.ts` |
| `web/src/views/AgentSkillROI.tsx` | Two stacked Recharts sections (by agent, by skill). | `api.ts`, Recharts |
| `web/src/views/ProviderMix.tsx` | Recharts vertical `BarChart` + raw-numbers table. | `api.ts`, Recharts |

**Bundle size**: 548 KB JS / 160 KB gzipped. Almost all of that is Recharts. We can `manualChunks` it later; for v0.1 it doesn't matter.

---

### Day 6 — Cross-platform packaging + docs

**The day's goal**: anyone with `curl | sh` can install shiptrace on darwin-arm64, darwin-amd64, linux-amd64, or windows-amd64.

| File | Purpose | Connects to |
|---|---|---|
| `scripts/build-release.sh` | Builds the React bundle first (so the embed is fresh), then cross-compiles all three binaries × four targets via `GOOS=<os> GOARCH=<arch> go build`. Packages each target into a `.tar.gz` (or `.zip` for Windows) containing the binaries + the installer + a `MANIFEST.txt`. Emits `SHA256SUMS`. Restores the `//go:embed` placeholder afterward (Vite's `emptyOutDir` wipes it). | Releases. |
| `scripts/install.sh` | POSIX shell installer. Two modes: GitHub-releases (default — resolves `latest` via the GitHub API, downloads the right archive) and `SHIPTRACE_LOCAL_DIST=...` (dev). Detects (os, arch), picks install dir intelligently (`~/.local/bin` if on `$PATH`, else `/usr/local/bin`), warns about missing `$PATH` entries, supports optional `SHIPTRACE_RUN_INIT=1`. | The wider world. |
| `README.md` (rewritten) | Insight-first lede. Sessions-to-ship as the missing metric. Curl-install one-liner. Provider/adapter status tables. Links to the three doc files. | The wider world. |
| `docs/privacy.md` | The privacy bible. Every field captured, by default vs. opt-in, where it lives on disk, how to capture less, how to capture more (env-var opt-ins). "Report regressions as P0." | Users. |
| `docs/adapters.md` | How to write your own ship adapter. Documents the JSONL contract, shows a ~25-line complete one-shot adapter in Go, explains the attribution chain, and lists what adapters MUST NOT do (write SQLite directly, capture verbatim text, dirty user repos, fail upstream commands). | Future contributors. |

**No new Go code on Day 6** — it's all shell, markdown, and packaging glue. That's by design; the code surface should stabilize before the docs lock it in.

The cross-compile works because every Go dependency is pure Go — including the SQLite driver (`modernc.org/sqlite`). No CGo toolchain on user machines.

---

### Day 7 — Dogfood + launch prep

**The day's goal**: run shiptrace on shiptrace, capture screenshots, draft the launch post. Tag v0.1.0.

| File | Purpose | Connects to |
|---|---|---|
| `scripts/dogfood-seed/main.go` | A one-off Go program that reads `git log --pretty=... --name-only` across the v0.0.0..v0.0.6 tag range, groups commits by chapter, and writes plausible session/prompt/tool_use/replan_signal/ship/session_stop events with **backdated** timestamps into a per-repo `SHIPTRACE_HOME` (`.shiptrace-dogfood/`, gitignored). | `events`, `eventlog`, `paths`, `ingest`, `store` |
| `docs/launch-post.md` | The launch post draft. Leads with one specific finding from the seeded data ("the two highest-replan days had the biggest initial plans") rather than the architecture. Honest about n=6 and the synthetic session boundaries. Marked `DRAFT — unpublished`. | The future. |
| `docs/screenshots/README.md` | Regen instructions for the dashboard screenshots. Explains why synthetic-data PNGs aren't committed: avoids implying we ran the tool for a month before launch. | Future doc maintainers. |
| `.git/hooks/post-commit` (installed live) | The actual post-commit hook in *this* repo. Every commit from Day 7 forward emits a real ship event into the user's `~/.shiptrace/events/`. The first two real ship events were written by the two Day-7 commits. | `cmd/shiptrace-git-postcommit` |

**This is the milestone day** — v0.1.0 is tagged, shiptrace is observing itself from this moment forward, and the launch post is sitting in the repo waiting for 2-3 weeks of live data before it's posted anywhere public.

---

## Cross-cutting concerns

These don't fit neatly into one day — they thread through the whole codebase.

### The attribution chain

When a `ship` event comes in without an explicit session id, shiptrace decides which session to attribute it to. The order is **deliberate and surfaced** (the dashboard shows `attribution_method` for every ship):

1. **Explicit** — `--session=ID` flag or `$SHIPTRACE_SESSION_ID` env. The user's word is the law.
2. **Per-project pointer** — the active CC session for this repo (kept fresh by every CC hook event, expires after 4 hours of inactivity).
3. **Global pointer** — the active manual session, if any.
4. **File overlap** — query `tool_events.files_touched` (JSON1 `json_each`) against the ship's file path, last 24h.
5. **Time window** — the most recent session that ended within 30 minutes of the ship's timestamp.
6. **None** — emit the event with no session id; the user corrects later.

The whole point of having this many levels is that **silent miscategorization is the only kind of footgun we can't live with**. Every level above "none" reports back the method it used, so a wrong attribution is visible.

### Privacy by default

Everything user-typed gets hashed, not stored verbatim. Prompt text → `sha256:<hex>`. Tool input → `sha256:<hex>`. Verbatim capture is two env vars away (`SHIPTRACE_LOG_PROMPT_TEXT=1`, `SHIPTRACE_LOG_TOOL_INPUT=1`), so opt-in requires intent every shell. v0.2 will add config-file equivalents and `redact_paths`.

### Performance budget

Two binaries are on hot paths and have **hard latency budgets**:

- `shiptrace-cc-hook`: <30 ms p99. Measured at 11 ms p99 on darwin-arm64.
- `shiptrace-git-postcommit`: <100 ms total (it's allowed to be slower; commits aren't that hot). Measured at ~60 ms including all the `git show` shell-outs.

Both binaries are **stdlib-only Go** to make this hit. No cobra, no SQLite, no fsnotify in those imports. The contract is enforced by code review.

### Testing strategy

- **Pure functions get unit tests** (`internal/replan`, `internal/display/relative_time`, `internal/events`).
- **I/O packages get round-trip tests** against `t.TempDir()` (`eventlog`, `session`, `paths`, `store`).
- **CLI packages get golden-output tests** where the test feeds args and asserts on a captured stdout (`cli/init`).
- **The CC hook and git adapter** get end-to-end tests that run real `git init` / `git commit` in temp repos.
- **`go test ./...`** runs all of it in under 5 seconds.

There are no mocks of the SQLite layer; tests just open a real SQLite in a temp dir. That's faster and more honest than mocking.

---

## Glossary (for non-native Go/JS devs)

| Term | What it means here |
|---|---|
| **Go package** | A directory full of `.go` files that share a `package <name>` declaration. Imported by full path: `github.com/LaurPl/shiptrace/internal/store`. |
| **`cmd/` vs `internal/`** | Go convention. `cmd/X/main.go` is the entrypoint for a binary named `X`. `internal/` is a magic directory name: anything inside it can be imported only by code in the same module. Keeps our internals private. |
| **`//go:embed`** | A compiler directive that pulls files into the binary at build time. We use it to embed the React bundle (`//go:embed all:web/dist`) and the SQL migrations (`//go:embed migrations/*.sql`). |
| **Cross-compile** | Building a binary for a different OS/arch on your own machine. `GOOS=linux GOARCH=amd64 go build` produces a Linux binary on macOS. Works because Go's standard library is platform-portable and our dependencies are pure-Go. |
| **CGo** | Go's mechanism for calling C code. We *don't* use it — every dependency is pure Go. This is what makes cross-compile trivial. |
| **WAL mode** (SQLite) | "Write-Ahead Logging" — concurrent reads and writes don't block each other. We turn it on at open time. |
| **JSON1 (`json_each`)** | A SQLite extension included with `modernc.org/sqlite` that lets you query JSON columns. We use it in `AttributeFile` to look up which session touched a file: `SELECT te.session_id FROM tool_events te, json_each(te.files_touched) j WHERE j.value = ?`. |
| **fsnotify** | Cross-platform file system notification library. macOS uses kqueue, Linux uses inotify, Windows uses ReadDirectoryChangesW — fsnotify hides those behind one Go API. The ingester uses it to tail the JSONL log. |
| **cobra** | A Go CLI framework. Each subcommand is a `&cobra.Command{...}` struct. We use it everywhere except in the CC hook and git post-commit binaries (where we want zero imports beyond stdlib). |
| **Vite / React / Recharts** | Vite is the JS build tool (it's what runs `npm run build` and produces `dist/`). React is the UI library. Recharts is a chart library built on React + D3. |
| **TypeScript** | JavaScript with type annotations, compiled away before the browser sees it. Our `web/src/api.ts` declares the shape of every JSON response. |
| **`//go:embed all:web/dist`** | The `all:` prefix tells `go:embed` to include hidden files (anything starting with `.`). Without it, we'd silently drop our `.placeholder`. |
| **`fsync`** | Forces the OS to flush buffered file writes to disk. The eventlog writer calls `fsync` after every line so a crash can't lose data. Microseconds-cheap on SSDs; we eat the cost for the durability guarantee. |
| **Atomic write (tmp + rename)** | Write to `foo.tmp`, then `rename("foo.tmp", "foo")`. On POSIX, the rename is atomic — observers see either the old file or the new file, never half-written. The pointer files and checkpoint files all use this. |

---

## What's not in this doc

- The actual SQL schema details — read `internal/store/migrations/0001_init.sql` and `0002_replan_signals.sql`. They're 40 lines total.
- The Cobra subcommand wiring in detail — read `internal/cli/root.go`, it's 50 lines.
- The exact event JSON shape — read `internal/events/event.go`, it's the canonical definition.
- The v0.2 roadmap — too speculative to commit to yet. The launch post hints at it.

If something here is unclear, the answer is usually two clicks deep in the codebase. The whole project is intentionally shallow.
