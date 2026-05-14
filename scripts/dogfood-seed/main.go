// Command dogfood-seed reconstructs shiptrace's own development history
// into the eventlog so the dashboard has real data to render without
// waiting 24 hours for an organic install to accumulate it.
//
// The data is honest in shape — every ship_event below uses a real commit
// SHA, real files-touched list, and real subject line from `git log`.
// The fabricated parts are the session boundaries (one session per
// chapter, names matching the build-plan tags), backdated wall-clock
// times so the dashboard's "last 24 hours" and "last week" buckets both
// have content, and the replan-signal counts (chosen to reflect the
// chapters where we actually had to course-correct mid-session).
//
// Usage:
//
//	SHIPTRACE_HOME=./.shiptrace-dogfood/ go run ./scripts/dogfood-seed
//	SHIPTRACE_HOME=./.shiptrace-dogfood/ ./shiptrace ingest --once
//
// Idempotent within a single run; re-running on top of an existing event
// log would duplicate events (we don't currently key on commit SHA in
// ship_events because the checkpoint guarantees the ingester never sees
// the same line twice).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/ingest"
	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/store"
)

// Chapter describes one day's session in the seed.
type Chapter struct {
	Tag           string        // git tag at the end of the chapter
	PrevTag       string        // git tag at the start
	Label         string        // user-facing session label
	StartOffset   time.Duration // hours before "now"
	Duration      time.Duration
	Hour          int           // wall-clock hour-of-day (UTC) the session started — varies for heatmap
	Agent         string        // optional agent that was active most of the session
	PromptCount   int           // synthesized; we never reconstruct prompt text
	ToolCallCount int           // synthesized
	PivotPhrases  []string      // replan_signal pivot_phrase events
	TodoWrites    []TodoSnap    // sequence of TodoWrite snapshots (drives reversal detection)
	Tokens        events.TokenCount
}

// TodoSnap is one TodoWrite payload snapshot.
type TodoSnap struct {
	Pending, InProgress, Completed, Total int
}

// Chapters is the schedule used by the seeder. The order matters: each
// session's StartOffset is "hours before now" so older chapters have
// larger offsets.
var Chapters = []Chapter{
	{
		Tag:     "v0.0.1", PrevTag: "v0.0.0",
		Label:   "Day 1 — manual recorder + events store",
		// Day 1 was 6.5 days ago, 9 AM UTC, ~3 hours.
		StartOffset: 6*24*time.Hour + 12*time.Hour,
		Duration:    3 * time.Hour,
		Hour:        9,
		Agent:       "Plan",
		PromptCount: 18, ToolCallCount: 47,
		Tokens:        events.TokenCount{In: 38000, Out: 12000},
		PivotPhrases:  nil,
		TodoWrites: []TodoSnap{
			{Pending: 11, InProgress: 0, Completed: 4, Total: 15},
			{Pending: 7, InProgress: 1, Completed: 7, Total: 15},
			{Pending: 0, InProgress: 0, Completed: 15, Total: 15},
		},
	},
	{
		Tag: "v0.0.2", PrevTag: "v0.0.1",
		Label:       "Day 2 — Claude Code hook + init/doctor",
		StartOffset: 5*24*time.Hour + 6*time.Hour,
		Duration:    4 * time.Hour,
		Hour:        14,
		Agent:       "claude-code-guide",
		PromptCount: 25, ToolCallCount: 63,
		Tokens:        events.TokenCount{In: 52000, Out: 18000},
		// Day 2 hit a regex bug that needed two retries and a privacy
		// reshape mid-session — that's where replans live.
		PivotPhrases: []string{"actually", "wait,"},
		TodoWrites: []TodoSnap{
			{Pending: 10, InProgress: 0, Completed: 0, Total: 10},
			{Pending: 4, InProgress: 1, Completed: 5, Total: 10},
			// Reversal: a "completed" item moved back to pending after the regex bug.
			{Pending: 5, InProgress: 1, Completed: 4, Total: 10},
			{Pending: 0, InProgress: 0, Completed: 10, Total: 10},
		},
	},
	{
		Tag: "v0.0.3", PrevTag: "v0.0.2",
		Label:       "Day 3 — git adapter + PR poller",
		StartOffset: 4*24*time.Hour + 8*time.Hour,
		Duration:    150 * time.Minute,
		Hour:        10,
		Agent:       "Plan",
		PromptCount: 19, ToolCallCount: 42,
		Tokens:        events.TokenCount{In: 41000, Out: 14500},
		PivotPhrases: []string{"hold on"},
		TodoWrites: []TodoSnap{
			{Pending: 10, InProgress: 0, Completed: 0, Total: 10},
			{Pending: 3, InProgress: 1, Completed: 6, Total: 10},
			{Pending: 0, InProgress: 0, Completed: 10, Total: 10},
		},
	},
	{
		Tag: "v0.0.4", PrevTag: "v0.0.3",
		Label:       "Day 4 — replan score + filesystem adapter",
		StartOffset: 3*24*time.Hour + 4*time.Hour,
		Duration:    2 * time.Hour,
		Hour:        16,
		Agent:       "Plan",
		PromptCount: 14, ToolCallCount: 34,
		Tokens:        events.TokenCount{In: 34000, Out: 11000},
		// A clean execution day — no pivots, no reversals.
		PivotPhrases: nil,
		TodoWrites: []TodoSnap{
			{Pending: 8, InProgress: 0, Completed: 0, Total: 8},
			{Pending: 0, InProgress: 0, Completed: 8, Total: 8},
		},
	},
	{
		Tag: "v0.0.5", PrevTag: "v0.0.4",
		Label:       "Day 5 — dashboard MVP",
		StartOffset: 2*24*time.Hour + 5*time.Hour,
		Duration:    4 * time.Hour,
		Hour:        11,
		Agent:       "frontend-design",
		PromptCount: 22, ToolCallCount: 51,
		Tokens:        events.TokenCount{In: 47000, Out: 16000},
		// Day 5 had a Vite outDir gotcha mid-session.
		PivotPhrases: []string{"actually"},
		TodoWrites: []TodoSnap{
			{Pending: 9, InProgress: 0, Completed: 0, Total: 9},
			{Pending: 3, InProgress: 1, Completed: 5, Total: 9},
			{Pending: 0, InProgress: 0, Completed: 9, Total: 9},
		},
	},
	{
		Tag: "v0.0.6", PrevTag: "v0.0.5",
		Label:       "Day 6 — packaging + docs",
		StartOffset: 30 * time.Hour, // 1.25 days ago — straddles the Today window
		Duration:    2 * time.Hour,
		Hour:        13,
		Agent:       "Plan",
		PromptCount: 11, ToolCallCount: 22,
		Tokens:        events.TokenCount{In: 28000, Out: 8500},
		PivotPhrases: nil,
		TodoWrites: []TodoSnap{
			{Pending: 7, InProgress: 0, Completed: 0, Total: 7},
			{Pending: 0, InProgress: 0, Completed: 7, Total: 7},
		},
	},
}

// CommitInfo is the slice of `git log` we materialize per commit.
type CommitInfo struct {
	SHA, ShortSHA, Subject, Author string
	Files                          []string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "dogfood-seed:", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := paths.Home()
	if err != nil {
		return err
	}
	eventsDir, err := paths.EventsDir()
	if err != nil {
		return err
	}
	w, err := eventlog.New(eventsDir)
	if err != nil {
		return err
	}
	defer w.Close()

	fmt.Println("seeding into:", home)

	now := time.Now().UTC()
	for _, ch := range Chapters {
		// Place the session at a specific hour-of-day in UTC, on the day
		// implied by StartOffset, so the heatmap fills in plausibly.
		base := now.Add(-ch.StartOffset)
		startTs := time.Date(base.Year(), base.Month(), base.Day(), ch.Hour, 0, 0, 0, time.UTC)
		endTs := startTs.Add(ch.Duration)

		sessionID := events.NewSessionID()
		commits, err := commitsInRange(ch.PrevTag, ch.Tag)
		if err != nil {
			return fmt.Errorf("commits %s..%s: %w", ch.PrevTag, ch.Tag, err)
		}

		fmt.Printf("  %s — %s (%d commits) @ %s UTC\n",
			ch.Tag, ch.Label, len(commits), startTs.Format("2006-01-02 15:04"))

		if err := emitSessionStart(w, sessionID, startTs, ch); err != nil {
			return err
		}

		// Spread synthetic prompt and replan events across the session.
		if err := emitReplanSignals(w, sessionID, startTs, endTs, ch); err != nil {
			return err
		}
		if err := emitPromptEvents(w, sessionID, startTs, endTs, ch.PromptCount); err != nil {
			return err
		}

		// Spread the (real) commits evenly across the session window so
		// the timeline shows ship events flowing through it.
		if err := emitShipEventsAcrossWindow(w, sessionID, startTs, endTs, commits); err != nil {
			return err
		}

		if err := emitSessionStop(w, sessionID, endTs, ch); err != nil {
			return err
		}
	}

	// One in-progress session "today" so the Today view has a live row.
	if err := emitTodaySession(w, now); err != nil {
		return err
	}

	// Ingest right away so the dashboard has something to show.
	dbPath, err := paths.DBPath()
	if err != nil {
		return err
	}
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()
	checkpointPath, err := paths.CheckpointPath()
	if err != nil {
		return err
	}
	ing := ingest.New(s, eventsDir, checkpointPath)
	if err := ing.IngestOnce(context.Background()); err != nil {
		return fmt.Errorf("ingest: %w", err)
	}

	fmt.Println("✓ seeded. Run `SHIPTRACE_HOME=", home, " shiptrace serve` to open the dashboard.")
	return nil
}

func emitSessionStart(w *eventlog.Writer, id string, ts time.Time, ch Chapter) error {
	return w.Append(events.Event{
		EventType: events.SessionStart,
		Ts:        ts,
		SessionID: id,
		Provider:  "claude-code",
		Project:   "shiptrace",
		Agent:     ch.Agent,
		Model:     "claude-opus-4-7",
		Label:     ch.Label,
		Tokens:    &ch.Tokens,
		Metadata: map[string]any{
			"provider_session_id": "cc-dogfood-" + ch.Tag,
			"cwd":                 "/Users/lau/work/shiptrace",
		},
	})
}

func emitSessionStop(w *eventlog.Writer, id string, ts time.Time, _ Chapter) error {
	return w.Append(events.Event{
		EventType: events.SessionStop,
		Ts:        ts,
		SessionID: id,
		Provider:  "claude-code",
	})
}

func emitReplanSignals(w *eventlog.Writer, id string, start, end time.Time, ch Chapter) error {
	all := make([]time.Time, 0, len(ch.PivotPhrases)+len(ch.TodoWrites))
	all = spreadTimes(start, end, len(ch.PivotPhrases)+len(ch.TodoWrites))

	idx := 0
	for _, phrase := range ch.PivotPhrases {
		if err := w.Append(events.Event{
			EventType: events.ReplanSignal,
			Ts:        all[idx],
			SessionID: id,
			Provider:  "claude-code",
			Metadata: map[string]any{
				"kind":   "pivot_phrase",
				"phrase": phrase,
				"weight": 1.0,
			},
		}); err != nil {
			return err
		}
		idx++
	}
	for _, t := range ch.TodoWrites {
		if err := w.Append(events.Event{
			EventType: events.ReplanSignal,
			Ts:        all[idx],
			SessionID: id,
			Provider:  "claude-code",
			Metadata: map[string]any{
				"kind":        "todowrite",
				"pending":     t.Pending,
				"in_progress": t.InProgress,
				"completed":   t.Completed,
				"total":       t.Total,
				"weight":      0.5,
			},
		}); err != nil {
			return err
		}
		idx++
	}
	return nil
}

func emitPromptEvents(w *eventlog.Writer, id string, start, end time.Time, count int) error {
	times := spreadTimes(start, end, count)
	for i, t := range times {
		if err := w.Append(events.Event{
			EventType: events.Prompt,
			Ts:        t,
			SessionID: id,
			Provider:  "claude-code",
			Metadata: map[string]any{
				"prompt_length": 80 + i*7,
				"prompt_hash":   fmt.Sprintf("sha256:dogfood-%d", i),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func emitShipEventsAcrossWindow(w *eventlog.Writer, id string, start, end time.Time, commits []CommitInfo) error {
	times := spreadTimes(start, end, len(commits))
	for i, c := range commits {
		meta := map[string]any{
			"kind":               "commit",
			"sha":                c.SHA,
			"short":              c.ShortSHA,
			"author":             c.Author,
			"subject":            c.Subject,
			"ref":                c.SHA,
			"attribution_method": "explicit",
		}
		if err := w.Append(events.Event{
			EventType:    events.Ship,
			Ts:           times[i],
			SessionID:    id,
			Provider:     "git",
			FilesTouched: c.Files,
			Metadata:     meta,
		}); err != nil {
			return err
		}
	}
	return nil
}

// emitTodaySession adds a fresh in-progress session so the Today timeline
// has a live row representing "running shiptrace on shiptrace right now."
func emitTodaySession(w *eventlog.Writer, now time.Time) error {
	id := events.NewSessionID()
	start := now.Add(-45 * time.Minute)
	if err := w.Append(events.Event{
		EventType: events.SessionStart,
		Ts:        start,
		SessionID: id,
		Provider:  "claude-code",
		Project:   "shiptrace",
		Agent:     "Plan",
		Model:     "claude-opus-4-7",
		Label:     "Day 7 — dogfood + launch prep",
		Metadata: map[string]any{
			"provider_session_id": "cc-dogfood-today",
		},
	}); err != nil {
		return err
	}
	// A couple of prompts and tool calls so the running session has content.
	for i, ts := range spreadTimes(start, now, 4) {
		if err := w.Append(events.Event{
			EventType: events.Prompt,
			Ts:        ts,
			SessionID: id,
			Provider:  "claude-code",
			Metadata: map[string]any{
				"prompt_length": 60 + i*9,
				"prompt_hash":   fmt.Sprintf("sha256:today-%d", i),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// spreadTimes returns n timestamps evenly distributed in [start, end].
// When n==1 we return the midpoint so a single event still lands inside
// the window, never on a boundary.
func spreadTimes(start, end time.Time, n int) []time.Time {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		mid := start.Add(end.Sub(start) / 2)
		return []time.Time{mid}
	}
	step := end.Sub(start) / time.Duration(n+1)
	out := make([]time.Time, n)
	for i := 0; i < n; i++ {
		out[i] = start.Add(step * time.Duration(i+1))
	}
	return out
}

// commitsInRange shells out to git log for the prev..tag range, parsing
// SHA, short SHA, author, subject, and files-touched.
func commitsInRange(prev, tag string) ([]CommitInfo, error) {
	cmd := exec.Command("git", "log", "--pretty=format:%H%x09%h%x09%an%x09%s", "--name-only", "--reverse", fmt.Sprintf("%s..%s", prev, tag))
	cmd.Dir = repoRoot()
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseGitLog(string(out)), nil
}

func parseGitLog(out string) []CommitInfo {
	var commits []CommitInfo
	var cur *CommitInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			if cur != nil {
				commits = append(commits, *cur)
				cur = nil
			}
			continue
		}
		// Heuristic: a header line has 3+ tab characters. Anything else
		// is a filename emitted by --name-only.
		if strings.Count(line, "\t") >= 3 {
			if cur != nil {
				commits = append(commits, *cur)
			}
			fields := strings.SplitN(line, "\t", 4)
			cur = &CommitInfo{SHA: fields[0], ShortSHA: fields[1], Author: fields[2], Subject: fields[3]}
		} else if cur != nil {
			cur.Files = append(cur.Files, line)
		}
	}
	if cur != nil {
		commits = append(commits, *cur)
	}
	return commits
}

// repoRoot finds the top of the shiptrace working tree. We resolve via
// the file the binary was built from (PWD) rather than `git rev-parse`
// so the seeder works whether or not the cwd is the repo root.
func repoRoot() string {
	wd, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return ""
		}
		wd = parent
	}
}

// Compile-time guard against an embed import we no longer need.
var _ = json.Marshal
