# shiptrace

> The missing layer between any AI coding/working agent and whatever counts as "shipped" in your domain.

When you run AI agents on real work, the metric nobody publishes is **sessions-to-ship** — how many agent sessions does a typical commit, post, lecture, or test-suite green take in this project? Joined to per-agent and per-skill data, that one number exposes which patterns actually pay off and which feel productive without producing.

shiptrace is a **local-first observability layer** that captures session events from your AI agents and joins them to whatever counts as shipping in your domain — git commits, file publishes, red-to-green test runs, scheduled posts, delivered lectures. It's an **instrument, not a coach**: the numbers are the product. There is no LLM "insights layer." There is no streak counter. There is no cloud sync. The tool never interrupts.

## What you get

```
$ shiptrace report --week
shiptrace report — last 7 day(s)
────────────────────────────────────────────────────────────
  sessions:           10
  ships:              4
  sessions-to-ship:   2.50

  project                sessions  ships    sess/ship avg replan
  ──────────────────────────────────────────────────────────────
  research                      5      1         5.00       0.00
  social                        4      3         1.33       0.00
  shiptrace                     1      0            —       0.18
```

Plus a local dashboard at `localhost:7777` with five views — today's timeline, sessions-to-ship distribution per project, a replan heatmap (project × hour-of-day), agent/skill ROI, and provider mix.

## Install

```sh
curl -sSL https://raw.githubusercontent.com/LaurPl/shiptrace/main/scripts/install.sh | sh
```

Then:

```sh
shiptrace init      # wire Claude Code hooks (optional but recommended)
shiptrace doctor    # verify everything is connected
shiptrace serve     # open http://127.0.0.1:7777
```

## What it captures

**By default, just metadata** — session start/stop, tool name, file paths, token counts, timestamps, plus a SHA-256 hash of prompt content. No verbatim prompt text. No tool input verbatim. See [`docs/privacy.md`](docs/privacy.md) for the full list.

| Provider           | How it's captured                         | Status      |
|--------------------|--------------------------------------------|-------------|
| Manual recorder    | `shiptrace session start \| ship \| stop` | ✓ v0.1       |
| Claude Code        | Native hook integration via `shiptrace init` | ✓ v0.1   |
| Codex CLI          | `/goal` boundaries + log tail              | ⌛ v0.2      |
| Cursor / Aider     | Filesystem watch + log tail                | ⌛ v0.2      |
| ChatGPT / web      | Manual recorder is the escape hatch        | ✓ v0.1       |
| Custom (SDK)       | Two-line Python / TS / Go wrapper          | ⌛ v0.2      |

## Ship adapters

| Adapter      | What counts as a ship                         | How                                         |
|--------------|------------------------------------------------|---------------------------------------------|
| git          | commits, merged PRs                            | `shiptrace adapter install git`             |
| filesystem   | file lands in a configured `ship_paths` glob   | `shiptrace adapter scan-fs`                 |
| manual       | anything                                       | `shiptrace ship "description"`              |
| obsidian     | `status:` frontmatter transitions              | ⌛ v0.2                                      |
| ci-junit     | red-to-green transitions in test reports       | ⌛ v0.2                                      |
| buffer/typefully | scheduled → published                      | ⌛ v0.2                                      |

Writing a custom adapter is two functions; see [`docs/adapters.md`](docs/adapters.md).

## Core metrics

- **Sessions-to-ship** — sessions per shipping event, per project. Lower is tighter shipping discipline.
- **Replan score** — how often work pivoted mid-session. Combines TodoWrite status-reversal detection with pivot-phrase regex on user prompts. Normalized 0–1, saturates at 1 - e^(-w/5) so a session with 50 pivots doesn't dwarf one with 10 in the final number.
- **Agent / skill ROI** — sessions-to-ship grouped by which agent or skill was active.
- **Provider mix** — same metric across Claude Code vs. Codex vs. Cursor on comparable work.

## What this is not

- Not a productivity coach.
- Not a quantified-self tool.
- Not an LLM-cost tracker — [`ccusage`](https://github.com/ryoppippi/ccusage) and friends already exist.
- Not a generic LLM observability platform — Langfuse / Helicone exist.
- Not a team analytics dashboard.

It is one specific thing: a session-to-ship join, for anyone running AI agents on real work, regardless of provider.

## Status

v0.1 — usable, dogfoodable, local-only. Recorder coverage for providers beyond Claude Code arrives in v0.2.

## License

MIT. Copyright (c) 2026 Laurentiu Emanuel Placinta.

## See also

- [`docs/design.md`](docs/design.md) — architectural reasoning
- [`docs/build-plan.md`](docs/build-plan.md) — the 7-day build sequence we followed
- [`docs/privacy.md`](docs/privacy.md) — exactly what is and isn't captured
- [`docs/adapters.md`](docs/adapters.md) — write your own ship adapter
