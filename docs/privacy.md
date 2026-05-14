# shiptrace — privacy

Privacy is a design constraint, not an afterthought. This document tells you exactly what data shiptrace captures, where it goes, and how to make it capture less.

## TL;DR

- Everything is **local-only**. There is no cloud sync, no telemetry, no phone-home. Nothing leaves your machine unless you explicitly export it.
- **Prompt text is not captured by default.** We store length and a SHA-256 hash of each prompt — useful for `same prompt twice?` queries, useless for reconstructing what you said.
- **Tool input is not captured by default.** Same treatment as prompts: length + hash.
- **Verbatim capture is opt-in** via env vars (Day 2) or `config.yaml` (Day 3+).

## What we store

| Field | Captured by default? | Where | Notes |
|---|---|---|---|
| Session start / stop timestamps | ✓ | SQLite + JSONL | Required for session boundaries. |
| Session label (manual recorder) | ✓ | SQLite + JSONL | You typed it yourself. |
| Project name | ✓ | SQLite + JSONL | From `--project` flag or `cwd` basename. |
| Provider name (`claude-code`, `manual`, …) | ✓ | SQLite + JSONL | Required for cross-provider analytics. |
| Model name | ✓ | SQLite + JSONL | From the hook payload, when present. |
| Agent / skill names (Claude Code) | ✓ | SQLite + JSONL | Free of personally-identifying content. |
| Prompt length (chars) | ✓ | JSONL only | Number, not text. |
| Prompt SHA-256 hash | ✓ | JSONL only | One-way. Cannot be inverted. |
| Tool name (`Edit`, `Bash`, `TodoWrite`, …) | ✓ | SQLite + JSONL | Identity of the action, not its payload. |
| Tool input SHA-256 hash | ✓ | JSONL only | Same as prompt hash. |
| `files_touched` paths | ✓ | SQLite + JSONL | Extracted from common tool-input shapes (`file_path`, `path`). |
| TodoWrite status counts (pending / in_progress / completed) | ✓ | SQLite (replan_signals) + JSONL | Counts only — no item text. |
| Pivot-phrase matches (`"actually"`, `"scrap that"`, …) | ✓ | SQLite (replan_signals) + JSONL | The matched phrase, not the full prompt. |
| Token counts (in / out) | ✓ when provider supplies them | SQLite + JSONL | A small number. |
| Commit SHAs and author | ✓ when git adapter is installed | SQLite (ship_events) + JSONL | From `git log -1`. |
| Ship-event descriptions | ✓ when you type them | SQLite + JSONL | You typed them yourself. |
| **Verbatim prompt text** | ✗ opt-in | JSONL only | `SHIPTRACE_LOG_PROMPT_TEXT=1` |
| **Verbatim tool input** | ✗ opt-in | JSONL only | `SHIPTRACE_LOG_TOOL_INPUT=1` |

## Where the data lives

- `~/.shiptrace/events/YYYY-MM-DD.jsonl` — append-only event log. Source of truth.
- `~/.shiptrace/shiptrace.db` — SQLite. Derived from the event log; reproducible from JSONL if deleted.
- `~/.shiptrace/.current-session` — pointer file for the manual recorder.
- `~/.shiptrace/project-pointers/<hash>.json` — per-project session pointer used by the git adapter. Hash is sha256-truncated-of-repo-root, so we never write into your repo.
- `~/.shiptrace/cc-sessions/<cc-uuid>` — one tiny file per Claude Code session, holding the shp_ id we mapped it to. Deleted on `Stop`.
- `~/.shiptrace/.ingest-checkpoint.json`, `.fs-state.json`, `.pr-merge-state.json` — adapter dedupe state.
- `~/.claude/settings.json` — modified by `shiptrace init` to register the hook commands. Other keys (`theme`, `permissions`, your existing hooks) are preserved.

Nothing else.

## Where the data does *not* live

- No cloud anywhere. There is no shiptrace.com server.
- No analytics. No crash reports. No "anonymous usage data."
- No background daemon polls a remote service.
- Your repo is untouched. The git post-commit hook lives in `.git/hooks/post-commit` (per-repo) and the pointer that maps a CC session to a repo lives under `~/.shiptrace/`, not under your project.

## How to capture less

| Concern | What to do |
|---|---|
| Don't run the Claude Code hook at all | Skip `shiptrace init`, or `shiptrace adapter uninstall git` for repo-level. |
| Remove a specific session | Delete the matching line in `~/.shiptrace/events/YYYY-MM-DD.jsonl` and rebuild SQLite with `rm ~/.shiptrace/shiptrace.db && shiptrace ingest --once`. |
| Stop capturing entirely | `rm -rf ~/.shiptrace` and remove the hooks from `~/.claude/settings.json`. shiptrace is stateless beyond `~/.shiptrace/`. |
| Audit what's stored right now | `cat ~/.shiptrace/events/$(date -u +%Y-%m-%d).jsonl` reads the source of truth in plain JSON. |
| Verify prompts aren't leaking | `grep -i prompt_text ~/.shiptrace/events/*.jsonl` should return nothing unless you explicitly opted in. |

## How to capture more (opt-in)

For debugging or research, you can enable verbatim capture by setting env vars on the **process that runs the hook** — that means either your shell (for the manual recorder) or the way Claude Code is launched (for the CC hook).

```sh
export SHIPTRACE_LOG_PROMPT_TEXT=1
export SHIPTRACE_LOG_TOOL_INPUT=1
```

These are deliberately env vars, not config.yaml settings, so opt-in requires intent every shell session. A YAML-based equivalent will arrive in v0.2 alongside per-project `redact_paths` (paths whose contents shiptrace will refuse to include in `files_touched`).

## How attribution works (and how it can be wrong)

A "ship event" without an explicit session id is matched to a session in this precedence order:

1. **Explicit tag** — `--session=ID` flag or `$SHIPTRACE_SESSION_ID`. Highest precedence.
2. **Per-project pointer** — the active CC session for this repo (kept fresh by every CC hook event; expires after 4 hours of inactivity).
3. **Global pointer** — the active manual session, if any.
4. **File overlap** — `tool_events.files_touched` joined against the ship's file path, within the last 24 hours.
5. **Time window** — the most recent session that ended within 30 minutes of the ship event.
6. **None** — emit the ship event with no session_id; you can correct it later.

The dashboard surfaces the resolved attribution method (`explicit`, `file_overlap`, `time_window`, etc.) so silent mis-attribution can't hide. If you ever see a ship event credited to the wrong session, that's a bug — please report it with the relevant JSONL line.

## Reporting issues

If you find a privacy regression (something captured that this document says isn't, or that we say is opt-in but is on by default), please open an issue: https://github.com/LaurPl/shiptrace/issues. Treat it as a P0 bug — privacy regressions are not "polish."
