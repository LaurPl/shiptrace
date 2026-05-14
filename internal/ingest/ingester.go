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

	return SaveCheckpoints(i.checkpointPath, checkpoints)
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
		return i.store.UpdateSessionStop(ctx, e)
	case events.Ship:
		return i.store.InsertShipEvent(ctx, e)
	case events.Prompt, events.ToolUse, events.ReplanSignal:
		// Day 2+ wires these up. For day 1, the checkpoint still advances so
		// we don't reprocess them later.
		return nil
	default:
		i.logf("ingest: unknown event_type %q (skipped)", e.EventType)
		return nil
	}
}
