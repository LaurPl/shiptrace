# Writing a shiptrace ship adapter

A "ship adapter" is anything that emits **ship events** into shiptrace's event log. The built-in adapters cover git commits, files landing in configured paths, merged PRs, and a universal manual escape hatch — but the contract is open: any program that appends a well-formed JSON line to the shiptrace event log is a ship adapter.

This document explains the contract, walks through what the built-in adapters do, and shows how to add your own.

## The contract

A ship event is one JSON line in `~/.shiptrace/events/YYYY-MM-DD.jsonl`:

```json
{
  "schema_version": "1",
  "event_type": "ship",
  "ts": "2026-05-14T10:23:11Z",
  "session_id": "shp_abc123def456",
  "provider": "your-adapter-name",
  "files_touched": ["path/to/published.md"],
  "metadata": {
    "kind": "file_landed",
    "ref": "/full/path/or/url",
    "attribution_method": "file_overlap",
    "your_custom_field": "anything"
  }
}
```

Required fields:

| Field            | What                                                              |
|------------------|--------------------------------------------------------------------|
| `schema_version` | Always `"1"` for v0.1.                                            |
| `event_type`     | Always `"ship"`.                                                  |
| `ts`             | RFC 3339 UTC timestamp.                                           |
| `provider`       | Your adapter name — `git`, `filesystem`, `obsidian`, `my-thing`. |
| `metadata.kind`  | A short tag the dashboard can group by: `commit`, `pr_merged`, `publish`, `red_to_green`, `manual`, … |

Recommended fields:

- `session_id` — the shiptrace session this ship belongs to. **Optional.** Adapters that can't resolve a session should leave it empty; the ingester will record it as unattributed and the user can correct via `shiptrace tag`.
- `files_touched` — paths relevant to this ship.
- `metadata.ref` — a stable identifier (commit SHA, PR URL, file path, …).
- `metadata.attribution_method` — one of `explicit`, `file_overlap`, `time_window`, or your own if you have a domain-specific method.

That's it. Append a line, exit. shiptrace's ingester picks it up via fsnotify within a second.

## Two patterns

### One-shot adapters (recommended)

Built like a unix tool: invoked from outside (cron, a git hook, a CI step), do their work, exit. Examples in this repo: `cmd/shiptrace-git-postcommit`, `shiptrace adapter pr-poll`, `shiptrace adapter scan-fs`.

Pros: trivial to reason about, easy to test, can run on a `launchd`/`cron` schedule, no resident process.

Pattern:

```go
package main

import (
    "fmt"
    "os"
    "time"

    "github.com/LaurPl/shiptrace/internal/eventlog"
    "github.com/LaurPl/shiptrace/internal/events"
    "github.com/LaurPl/shiptrace/internal/paths"
)

func main() {
    eventsDir, err := paths.EventsDir()
    if err != nil { fail(err) }
    w, err := eventlog.New(eventsDir)
    if err != nil { fail(err) }
    defer w.Close()

    if err := w.Append(events.Event{
        EventType: events.Ship,
        Ts:        time.Now().UTC(),
        Provider:  "my-adapter",
        Metadata: map[string]any{
            "kind": "publish",
            "ref":  "https://example.com/post/123",
        },
    }); err != nil {
        fail(err)
    }
}

func fail(err error) {
    fmt.Fprintln(os.Stderr, "my-adapter:", err)
    os.Exit(1)
}
```

That's a complete adapter. Compile it, drop the binary on `$PATH`, run it from whatever upstream system tells you a publish happened.

### Long-running adapters

Watch a service or filesystem, emit events as state changes. The shiptrace daemon (`shiptrace ingest`) is itself this shape, but for ship adapters we recommend one-shot + cron until you have a concrete reason not to. The complexity budget per adapter is small.

## Attribution

The dashboard surfaces "which session did this ship belong to" using a precedence chain:

1. **Explicit** — your adapter set `session_id` directly (preferred).
2. **File overlap** — the ingester joins `files_touched` against recent `tool_events`.
3. **Time window** — the most recent session that ended within 30 minutes of `ts`.
4. **None** — the ship is unattributed; the user can correct via `shiptrace tag` (planned for v0.2).

If your adapter has a strong notion of which session was "active" (e.g. you read the per-project pointer at `~/.shiptrace/project-pointers/<sha256-of-cwd>.json`), set `session_id` directly and `metadata.attribution_method = "explicit"`. Don't guess — the user prefers a `⚠ unattributed` warning over a silent miscategorization.

## Per-project session pointers

Adapters that operate inside a specific working directory can read the pointer that the Claude Code hook writes:

```go
import (
    "github.com/LaurPl/shiptrace/internal/paths"
    "github.com/LaurPl/shiptrace/internal/session"
)

home, _ := paths.Home()
pointerPath, _ := session.ProjectPointerPath(home, "/path/to/repo")
ptr, _ := session.ReadActive(pointerPath)
if ptr != nil && !ptr.IsStale(time.Now(), session.DefaultMaxStaleness) {
    // ptr.SessionID is the active shp_ id
}
```

The pointer is keyed by the git repo root (or the cwd, if not in a repo), hashed so we never write into the user's project. Day 3's `internal/adapters/git` uses this directly — read its source for a complete example.

## Where to put your code

If you're contributing back to shiptrace:

- `internal/adapters/<name>/` for the package
- `cmd/shiptrace-<name>-…/` for any standalone binary
- An entry in `internal/cli/adapter.go` if the adapter has user-facing subcommands

If you're keeping it private:

- Build to a standalone binary, name it anything, drop it on `$PATH`.
- shiptrace will happily ingest events from any provider name. No registration.

## Testing

The cleanest test for a new adapter:

1. `SHIPTRACE_HOME=$(mktemp -d) ./your-adapter`
2. Verify `~/.shiptrace/events/$(date -u +%Y-%m-%d).jsonl` contains the expected JSON line.
3. `SHIPTRACE_HOME=… shiptrace ingest --once`
4. `sqlite3 ~/.shiptrace/shiptrace.db "SELECT * FROM ship_events"` and confirm the row materializes.

If the adapter is in this repo, mirror the test patterns in `internal/adapters/git/postcommit_test.go` and `internal/adapters/filesystem/scanner_test.go`.

## Things adapters should not do

- **Don't write to `~/.shiptrace/shiptrace.db` directly.** Only the ingester writes to SQLite; that's the invariant that keeps "JSONL is source of truth" trustworthy.
- **Don't capture prompt text or tool-input verbatim** unless you've explicitly checked the `SHIPTRACE_LOG_PROMPT_TEXT` / `SHIPTRACE_LOG_TOOL_INPUT` env vars or equivalent config knobs. See [`docs/privacy.md`](privacy.md).
- **Don't add `.shiptrace/` directories into user repos.** The session pointer for the git adapter lives under `~/.shiptrace/project-pointers/<hash>.json` precisely to keep `git status` clean.
- **Don't fail the upstream command if shiptrace is missing.** Post-commit hooks should `|| true` the shiptrace call so a missing binary never blocks `git commit`. See `internal/adapters/git/installer.go` for the pattern.

## Coming in v0.2

- A formal SDK in Python, TS, and Go: `shiptrace.start_session()`, `shiptrace.tool_call()`, `shiptrace.ship()`.
- Provider plugins for Codex, Cursor, Aider via the same hook-or-tail recipe.
- `shiptrace tag <session>` for user-driven attribution correction.
