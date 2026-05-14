# shiptrace — Design

> The missing layer between any AI coding/working agent and whatever counts as "shipped" in your domain.

## What it is

A local-first observability layer that records what your AI agents did, joins those events to what actually landed, and surfaces the metrics nobody else publishes. Code is one supported domain; writing, QA, content, design, and consulting are equal citizens.

The core insight: **the unit of work isn't the prompt, it's the session.** And the metric nobody publishes is *sessions-to-ship* — how many sessions does a typical commit/publish/ship take in this project? Joined to per-agent and per-skill data, this exposes which patterns actually pay off and which feel productive without producing.

## Core metrics surfaced

- **Sessions-to-ship** — sessions per shipping event, by project. Lower = tighter shipping discipline.
- **Replan score** — how often work pivots mid-session. Proxies for thrash vs. progress.
- **Agent / subagent effectiveness** — sessions-to-ship grouped by which agent was active. Reveals whether orchestration patterns actually help.
- **Skill ROI** — same idea for skills. Reveals which `.claude/skills/` files pull weight and which are dead code.
- **Time-to-first-write** — how long before a session does anything mutating. High values flag exploration-mode sessions.
- **Read/write tool ratio** — proxy for explore-vs-ship mode within a session.

## Architecture (three layers, deliberately decoupled)

```
┌─────────────────────────────────────────────────────────────┐
│  Layer 1: Recorders                                          │
│  ──────────────────                                          │
│  Per-provider scripts that capture session events and        │
│  normalize them into the canonical schema.                   │
│                                                              │
│  claude-code  codex  cursor-watcher  aider  manual  sdk      │
└──────────────────────────┬──────────────────────────────────┘
                           │
                  Unix socket / append-only JSONL
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  Layer 2: Core Events Store                                  │
│  ──────────────────────────                                  │
│  Provider-neutral schema. SQLite for queries, JSONL          │
│  as source of truth so corruption is recoverable.            │
│                                                              │
│  events / sessions / tool_events / ship_events               │
└──────────────────────────┬──────────────────────────────────┘
                           │
                  Attribution joins
                           │
┌──────────────────────────▼──────────────────────────────────┐
│  Layer 3: Ship Adapters                                      │
│  ─────────────────────                                       │
│  Plug-in points that emit `ship` events into the store.     │
│                                                              │
│  git  filesystem  obsidian  ci-junit  buffer  email  manual │
└─────────────────────────────────────────────────────────────┘
```

Communication: one local Unix socket at `~/.shiptrace/events.sock` (or platform equivalent on Windows/macOS). Any recorder, any adapter, any provider — they all just write events.

## Canonical event schema

```json
{
  "schema_version": "1",
  "event_type": "session_start | prompt | tool_use | replan_signal | session_stop | ship",
  "ts": "2026-05-14T10:23:11Z",
  "session_id": "shp_8f3a...",
  "provider": "claude-code | codex | cursor | aider | manual | ...",
  "project": "social-growth-guild",
  "agent": "instagram-strategist",
  "skill": "locale-romania",
  "model": "claude-opus-4-7",
  "tool": "Edit",
  "tool_input_hash": "sha256:...",
  "files_touched": ["path/to/file.md"],
  "tokens": {"in": 1240, "out": 380},
  "metadata": { }
}
```

Provider differences live in `metadata`. Core queries never need to know which agent fired an event.

## Per-provider capture strategies

| Provider | Recorder type | Notes |
|---|---|---|
| Claude Code | Native hooks (`SessionStart`, `UserPromptSubmit`, `PostToolUse`, `SubagentStop`, `Stop`) | Richest data; gets agent/skill/subagent context for free. **Build this second** — after manual recorder. |
| Codex CLI | `/goal` boundaries + log tail | Cleaner session delimiters; less granular than CC. |
| Cursor | Filesystem watch + chat-log tail | No hooks API; sessions inferred from edit bursts. Lossy but useful. |
| Aider | git-commit-per-turn + log tail | Natural mini-sessions per turn. |
| Continue / Cline / local models | Log tail + FS watch | Same pattern as Cursor. |
| ChatGPT / Claude.ai web | Manual only | User runs `shiptrace session start "drafting in web"`, then `shiptrace ship "..."`. |
| Custom agents via API | SDK wrapper (Python, TS, Go) | Two lines: `shiptrace.start_session()` and `shiptrace.tool_call()`. |

Recorders ship as separate small binaries/scripts so users install only what they need.

## Ship adapters (per domain)

| Adapter | What counts as a ship | Attribution method |
|---|---|---|
| **git** | commit, PR opened, PR merged | Post-commit hook writes commit SHA + active session ID |
| **filesystem** | file lands in user-configured "ship paths" | File-overlap with session's tool_use events |
| **obsidian** | note frontmatter `status` transitions to `published`/`live`/`shared` | File watcher + frontmatter parser |
| **ci-junit** | red-to-green transition in test report | Watch report dir; detect failure count delta |
| **buffer / typefully / hypefury** | post moves from scheduled to published | API poll; match content hash to session output |
| **email** | outbound message sent (and optionally positive reply) | User runs `shiptrace tag-outbound <session>` |
| **manual** | anything | `shiptrace ship "description"` — universal escape hatch |

## Attribution model (three methods, in precedence order)

When a ship event arrives without an explicit session ID:

1. **Explicit tag** (user-set via `shiptrace tag` or `shiptrace ship`) — highest precedence.
2. **File overlap** — session touched files A, B, C; ship event involves those files → attribute to that session.
3. **Time window** — any ship within N minutes after a `Stop` event belongs to that session (fallback).

The dashboard surfaces the attribution chain so the user can correct mis-attributions, and the system should eventually learn from corrections.

## Replan score (the genuinely novel metric)

Three signal sources, combined into a single per-session score:

1. **Crude**: count `TodoWrite` invocations per session.
2. **Better**: parse TodoWrite payloads from transcript; detect *status reversals* (`in_progress` → `pending`, `completed` → `pending`) and *task deletions vs additions*. Reversals are the real signal.
3. **Best**: count `Stop` events followed by a new `UserPromptSubmit` within N seconds where the prompt contains pivot phrases ("actually", "wait", "let's instead", "scrap that", "ignore that"). Cheap regex.

Combine into a normalized 0–1 score per session. Project-level and time-of-day aggregations are where this metric shines.

## Configuration

`~/.shiptrace/config.yaml`:

```yaml
projects:
  social-growth-guild:
    paths: [/home/lau/projects/social-growth-guild]
    ship_paths:
      - /home/lau/projects/social-growth-guild/scheduled/**
      - /home/lau/projects/social-growth-guild/published/**
    adapters: [filesystem, buffer]
    mode: production       # vs. exploration

  research-institute:
    paths: [/home/lau/vaults/research-institute]
    adapters: [obsidian]
    obsidian:
      ship_on_status: [published, live, shared]
    mode: exploration       # track sessions but don't expect ships

  qa-client-acme:
    paths: [/home/lau/clients/acme]
    adapters: [git, ci-junit]
    ci_junit:
      report_dir: /home/lau/clients/acme/test-results
      ship_on: red_to_green

  philosophy-lectures:
    paths: [/home/lau/teaching/noua-acropola]
    adapters: [manual, filesystem]
    ship_paths: [/home/lau/teaching/noua-acropola/delivered/**]

privacy:
  log_prompt_text: false           # default: store length + hash only
  log_tool_input_text: false
  redact_paths: [/home/lau/.secrets/**]

retention:
  raw_events_days: 90
  rollups_days: forever
```

## Install footprint

```
~/.shiptrace/
  config.yaml
  shiptrace.db                  # SQLite
  events/
    2026-05-14.jsonl
    ...
  recorders/
    claude-code (binary)
    codex (binary)
    cursor-watcher (binary)
    manual (binary)
  adapters/
    git (binary)
    filesystem (binary)
    obsidian (binary)
    ci-junit (binary)
    buffer (binary)

~/.claude/
  hooks/                        # written by `shiptrace init`
  settings.json                 # appended, not replaced
```

Per-project: a `.shiptrace/` directory holding only the current-session pointer file. Nothing else added to user repos.

## User-facing commands

```bash
shiptrace init                       # detect providers, scaffold config
shiptrace doctor                     # verify hooks, watchers, adapters
shiptrace session start "..."        # manual: open a session
shiptrace session stop               # manual: close a session
shiptrace ship "..."                 # manual: log a ship event
shiptrace tag <session-id>           # tag the next outbound event with this session
shiptrace report [--day|--week|--month]    # CLI summary
shiptrace serve                      # start local dashboard on :7777
shiptrace export --format markdown --month [--anonymous]
```

## Privacy & non-goals (explicit)

These are design constraints, not afterthoughts:

- No cloud sync unless explicitly enabled.
- No prompt content captured by default — lengths and hashes only.
- **No LLM "insights layer" on top.** The numbers are the product. Adding GPT-summarizes-your-Claude-data is exactly the slide into productivity-coaching SaaS we're avoiding.
- No notifications. The tool never interrupts.
- No streaks, no gamification, no nudges.
- No telemetry phoning home. Crash reports opt-in and reviewable.

The tool should look like an instrument, not a coach.

## Hard constraints

- **Hook latency**: <30ms p99 per hook invocation. This pushes recorders toward Go or Rust even if the dashboard is JS.
- **"Shipped" is ambiguous**: track commit, PR opened, PR merged, deployed separately. Let the user pick the one that defines "shipped" for them.
- **Session boundaries are fuzzy**: a unit of work can span multiple sessions if the user `/clear`s or restarts. Add a `work_unit` concept on top of sessions, inferred from branch name or `TodoWrite` continuity.
- **Exploration mode must look intentional**: a project flagged `exploration` with zero ships should render as ✓ in the dashboard, not ✗. The alternative encourages inventing ship events to feed the metric (Goodhart's Law).

## Tech stack (recommended)

- **Hook binaries**: Go. Compiled, fast cold start, easy single-file distribution, cross-platform.
- **Core daemon / ingester**: Go.
- **Storage**: SQLite for queries, JSONL append-only for raw events.
- **Dashboard**: Local web server (likely Go HTTP server serving static React) on `localhost:7777`. Recharts for visualizations. Five views maximum.
- **CLI**: same Go binary, subcommands.
- **License**: MIT.

## Dashboard views (five total, resist scope creep)

1. **Today** — live timeline of sessions across all projects, color-coded by ship/no-ship/in-progress.
2. **Distribution** — sessions-to-ship histogram per project, side-by-side.
3. **Replan heatmap** — project × hour-of-day, replan-score-weighted.
4. **Agent/skill ROI** — bar chart, sessions-to-ship grouped by which agent/skill was active.
5. **Provider mix** — same metric across Claude Code vs Codex vs Cursor on comparable work.

## Project metadata

- **Name**: shiptrace
- **License**: MIT
- **Repo**: github.com/<user>/shiptrace
- **Domain**: shiptrace.dev (pending availability check)
- **Author**: Lau, freelance developer/QA practitioner based in Romania
- **Audience**: anyone running AI agents across multiple kinds of work who wants to see what actually shipped

## What this is not

- Not a productivity coach.
- Not a quantified-self tool.
- Not an LLM-cost tracker (ccusage etc. already exist).
- Not a generic LLM observability platform (Langfuse, Helicone exist).
- Not a team analytics dashboard.

It is one specific thing: a session-to-ship join, for anyone running AI agents on real work, regardless of provider.
