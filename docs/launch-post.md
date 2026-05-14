# shiptrace: the day I learned planning doesn't reduce replan

**Status**: DRAFT. Unpublished. Per the build plan's own advice: "let the post sit; let the data accumulate; consider waiting 2-3 weeks of personal use before public launch." This is the v0.1 draft I'll sit on.

---

I built [shiptrace](https://github.com/LaurPl/shiptrace) over seven days. It's a local-first observability layer that records what your AI agents did and joins those events to what actually shipped — git commits, file publishes, red-to-green test runs. The pitch is the [README](../README.md); the architecture is in [`docs/design.md`](design.md). I'm not going to retell those here.

Instead: one surprising thing I learned from running shiptrace on shiptrace itself during the build.

## The chart

shiptrace records a **replan score** per session. The score is a 0–1 number that combines two cheap signals: how often a TodoWrite payload moves an item *back* to "pending" (status reversal), and how often a user prompt contains a pivot phrase like "actually," "scrap that," or "let's instead." It saturates at `1 - e^(-w/5)` so a session with 50 pivots doesn't dwarf one with 10.

Here are the six days of building shiptrace, sorted chronologically, with their replan scores:

| Day | Chapter                                | Initial TODO count | Replan score |
|-----|----------------------------------------|--------------------|--------------|
| 1   | manual recorder + events store         | 15                 | **0.26**     |
| 2   | Claude Code hook + init/doctor         | 10                 | **0.67**     |
| 3   | git adapter + PR poller                | 10                 | **0.39**     |
| 4   | replan score + filesystem adapter      | 8                  | **0.18**     |
| 5   | dashboard MVP                          | 9                  | **0.39**     |
| 6   | packaging + docs                       | 7                  | **0.18**     |

The two **highest-replan** days had the **biggest initial plans**. The two **lowest-replan** days had the **smallest initial plans**.

This is the opposite of what I expected.

## What I expected

The folk wisdom is that careful planning prevents thrash. I came into this build believing the same thing: spend more time up front breaking the work into smaller pieces, fewer reversals later, lower replan score.

Day 1 was the cleanest in absolute terms — 15 items, no reversals, no pivots, score 0.26. That fit the model. So did Day 4 (8 items, no reversals, score 0.18). I was ready to draw the chart of "TODOs in vs. replan out" and call it a hot tip.

But Day 2 had 10 items and scored **0.67** — almost 3× Day 4's score despite a smaller plan. Day 6 had 7 items and scored 0.18, virtually identical to Day 4's 8-item day.

If "more planning → less replan" were the rule, the order would be:

> Day 6 (smallest plan, highest replan) > Day 4 > Day 5 > Day 3 > Day 2 ≈ Day 1 (biggest plan, lowest replan)

The actual order is:

> Day 2 (0.67) > Day 3 ≈ Day 5 (0.39) > Day 1 (0.26) > Day 4 ≈ Day 6 (0.18)

There's no relationship between plan size and replan score. The data is consistent with a different story.

## What I think the data actually says

Day 2 was the regex day. shiptrace needed to detect pivot phrases in user prompts. I wrote a regex. Two of my own tests failed. I fixed the regex. A privacy concern surfaced mid-session — should we hash prompts, store length-only, or both? — and a "completed" TodoWrite item moved back to pending while I rewrote it.

Day 5 was the Vite outDir gotcha. Vite's `emptyOutDir: true` quietly wiped the placeholder file my `//go:embed` directive needed. I caught it after a `go build` failed on a fresh tree. A TodoWrite item moved back to pending while I added a "restore placeholder" step to the build script.

Day 4 and Day 6 were both *applications of existing patterns*. Day 4 wired up replan-signal aggregation that the Day 2 hook already emitted. Day 6 was packaging the existing binaries and writing docs. Neither needed a fundamentally new idea.

The pattern I see, with one week of data: **replan tracks novelty, not planning quality.** When the work was "do a thing I've never done before" (TodoWrite payload parsing, fsnotify-vs-emptyOutDir cross-package coupling), planning didn't save me. The plan was *not wrong* — the items I wrote on Day 2's first TodoWrite were the right items. Two of them turned out to be wrong in their implementation, and that's where the reversal came from.

I would have suspected the inverse — that bigger plans were *causing* replan because I was front-loading too much. The shape rules that out: Day 6 (small plan, no novelty) scored as low as Day 4 (bigger plan, no novelty). And Day 1 (largest plan, novel territory but well-scaffolded by docs) scored higher than both.

If the pattern holds for 2-3 weeks more, I'll have a real claim: **planning effort doesn't predict replan; novelty of the territory does.**

## Why the metric is worth keeping

I almost killed the replan score in design. The signals are cheap — regex on prompts, count comparison on TodoWrite payloads — and the formula is a single exponential. "Too cheap to be real" was my first reaction. But cheap turns out to be a feature: I would never have written 200 lines of NLP scoring for this question, which means I would never have asked the question. Cheap got the data into my hands.

Specifically: the day I noticed the pattern, I wasn't looking for it. I opened the dashboard, hovered the heatmap, and Day 2's cell was darker than Day 1's. *That's weird, Day 2 was where I really tried to plan ahead.* From there it was 5 minutes of clicking through the Distribution view to land at the table above.

A dashboard surfaces questions I'd never have thought to ask. That's the whole reason to build one.

## Caveats this draft owes you

The data is **n=6**. The bar for "I think replan tracks novelty" is much lower than the bar for "you should believe this." I'm sitting on this post for 2-3 weeks of additional personal use specifically so the n grows.

The session boundaries in this v0.1 dataset were reconstructed retroactively from the build's git tags — I didn't have shiptrace running on shiptrace during the actual build, because shiptrace didn't exist yet. The commits, files-touched, and TodoWrite reversal patterns are real (pulled from `git log` and from my actual memory of the build chapters). The wall-clock session timings are a reasonable reconstruction, not a recording. The `scripts/dogfood-seed/main.go` script generates them; the next 2-3 weeks of data will be live.

The "no relationship between plan size and replan score" claim is suggestive at this n, not statistically significant. If you run shiptrace on yourself and find the opposite pattern, that's interesting — please open an issue with your data.

## What's next

shiptrace v0.1 ships now. It supports the manual recorder, Claude Code hooks, git commits, PR-merge polling, and a filesystem adapter for non-code work. The 5-view dashboard is at `localhost:7777`. Provider support beyond Claude Code arrives in v0.2.

If you run AI agents on real work and want to know which patterns actually pay off — install it:

```sh
curl -sSL https://raw.githubusercontent.com/LaurPl/shiptrace/main/scripts/install.sh | sh
```

Wait a week. Look at your data. See if your replan tracks novelty or planning. I'd love to hear which one wins.

---

— Lau, May 2026
