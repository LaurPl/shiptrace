package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

// Ingester tails the daily JSONL files under eventsDir and materializes the
// events it cares about into the store. The checkpoint file persists per-file
// byte offsets so a crash-restart resumes without double-applying.
type Ingester struct {
	store          *store.Store
	eventsDir      string
	checkpointPath string

	// debounce controls how long we coalesce a burst of fsnotify writes
	// before running IngestOnce. Configurable for tests.
	debounce time.Duration

	// now is the clock the staleness sweep reads. Injectable so tests can
	// drive deterministic staleness without sleeping. Defaults to UTC wall.
	now func() time.Time

	// staleAfter is how long a session may sit with no activity and no
	// session_stop before the sweep finalizes it. Defaults to
	// store.DefaultStaleAfter; settable for tests and future config wiring.
	staleAfter time.Duration

	// logf receives non-fatal status messages (e.g. malformed lines, dropped
	// event types). Defaults to discarding; the CLI wires it to stderr.
	logf func(format string, args ...any)
}

// New constructs an Ingester. The eventsDir must exist (paths.EventsDir()
// ensures this). The checkpointPath is created on first save.
func New(s *store.Store, eventsDir, checkpointPath string) *Ingester {
	return &Ingester{
		store:          s,
		eventsDir:      eventsDir,
		checkpointPath: checkpointPath,
		debounce:       50 * time.Millisecond,
		now:            func() time.Time { return time.Now().UTC() },
		staleAfter:     store.DefaultStaleAfter,
		logf:           func(string, ...any) {},
	}
}

// SetLogger wires a logger for non-fatal events. Pass nil to suppress.
func (i *Ingester) SetLogger(logf func(format string, args ...any)) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	i.logf = logf
}

// SetClock overrides the clock the staleness sweep reads. For tests. Passing
// nil restores the default UTC wall clock.
func (i *Ingester) SetClock(now func() time.Time) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	i.now = now
}

// SetStaleThreshold overrides how long a session may be idle (no activity, no
// session_stop) before the sweep finalizes it. A value <= 0 disables the sweep.
func (i *Ingester) SetStaleThreshold(d time.Duration) {
	i.staleAfter = d
}

// IngestOnce performs a single sweep: load checkpoints, scan every JSONL file
// from its checkpoint, dispatch events into the store, save checkpoints. Used
// by both `ingest --once` and as the per-tick action of Run.
func (i *Ingester) IngestOnce(ctx context.Context) error {
	checkpoints, err := LoadCheckpoints(i.checkpointPath)
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(i.eventsDir)
	if err != nil {
		return fmt.Errorf("ingest: read events dir: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		fullPath := filepath.Join(i.eventsDir, name)
		startOffset := checkpoints[name]
		newOffset, scanErr := eventlog.ScanFile(fullPath, startOffset, func(e events.Event, nextOffset int64) error {
			if err := i.dispatch(ctx, e); err != nil {
				return err
			}
			checkpoints[name] = nextOffset
			return nil
		})
		if scanErr != nil {
			// Save what we got so far so we don't redo applied events on retry.
			_ = SaveCheckpoints(i.checkpointPath, checkpoints)
			return fmt.Errorf("ingest: scan %s: %w", name, scanErr)
		}
		checkpoints[name] = newOffset
	}

	if err := SaveCheckpoints(i.checkpointPath, checkpoints); err != nil {
		return err
	}

	// Finalize any sessions left open without a session_stop (SIGKILL, window
	// close, crash, OS shutdown — Claude Code's SessionEnd hook doesn't fire on
	// those). This runs after the checkpoint save and is best-effort: a sweep
	// failure is logged, never returned, so it can't undo durable ingest
	// progress. The next pass retries. Note: an abandoned session writes no new
	// JSONL, so it won't itself trigger an fsnotify pass under Run — it gets
	// swept on the next pass any other activity triggers, or on `ingest --once`
	// / a rebuild. That eventual-consistency latency is acceptable; a session
	// reading "running" a little longer is not a correctness fault.
	i.sweepStale(ctx)
	return nil
}

// sweepStale runs the staleness sweep and logs a one-line summary when it
// changed anything. Errors are non-fatal by design (see IngestOnce).
func (i *Ingester) sweepStale(ctx context.Context) {
	res, err := i.store.SweepStaleSessions(ctx, i.now(), i.staleAfter)
	if err != nil {
		i.logf("ingest: stale sweep: %v", err)
		return
	}
	if res.Changed() {
		i.logf("ingest: stale sweep finalized %d, reopened %d session(s)",
			len(res.Finalized), len(res.Reopened))
	}
}

// Run is the long-running tail daemon: catch up, then react to fsnotify
// events on the eventsDir, debounced. Returns when ctx is cancelled.
func (i *Ingester) Run(ctx context.Context) error {
	if err := i.IngestOnce(ctx); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("ingest: fsnotify: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(i.eventsDir); err != nil {
		return fmt.Errorf("ingest: watch %s: %w", i.eventsDir, err)
	}

	var pendingTimer *time.Timer
	fire := func() {
		if err := i.IngestOnce(ctx); err != nil {
			i.logf("ingest: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			if pendingTimer != nil {
				pendingTimer.Stop()
			}
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// We only care about writes/creates/renames of .jsonl files.
			if !strings.HasSuffix(event.Name, ".jsonl") {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			if pendingTimer != nil {
				pendingTimer.Stop()
			}
			pendingTimer = time.AfterFunc(i.debounce, fire)
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			i.logf("ingest: watcher: %v", err)
		}
	}
}

func (i *Ingester) dispatch(ctx context.Context, e events.Event) error {
	switch e.EventType {
	case events.SessionStart:
		return i.store.UpsertSessionStart(ctx, e)
	case events.SessionStop:
		if err := i.store.UpdateSessionStop(ctx, e); err != nil {
			return err
		}
		// Once we know the session ended, compute its replan score from
		// whatever signals have been ingested so far. The dashboard reads
		// the persisted score directly, so this is the moment to lock it in.
		if _, err := i.store.ComputeAndStoreReplanScore(ctx, e.SessionID); err != nil {
			i.logf("ingest: replan_score for %s: %v", e.SessionID, err)
		}
		return nil
	case events.Ship:
		return i.store.InsertShipEvent(ctx, e)
	case events.Prompt:
		return i.store.BumpSessionPromptCount(ctx, e.SessionID)
	case events.ToolUse:
		return i.store.InsertToolEvent(ctx, e)
	case events.ReplanSignal:
		return i.store.InsertReplanSignal(ctx, e)
	default:
		i.logf("ingest: unknown event_type %q (skipped)", e.EventType)
		return nil
	}
}
