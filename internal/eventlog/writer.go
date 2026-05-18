// Package eventlog is the append-only JSONL source of truth for shiptrace
// events. The store (SQLite) is a derived materialized view; if it gets
// corrupted, the ingester rebuilds it from these files. Treat that invariant
// as load-bearing.
package eventlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

// Writer appends events to a daily-rotated JSONL file under a base directory.
// One Writer per process is the intended pattern; cross-process concurrent
// writes rely on POSIX O_APPEND atomicity and are revisited in week 2.
type Writer struct {
	dir  string
	mu   sync.Mutex
	f    *os.File
	date string // YYYY-MM-DD, UTC, of the currently open file

	// now is injected to make rollover testable. Production callers leave it nil.
	now func() time.Time
}

// New constructs a Writer rooted at dir. The directory must already exist;
// callers typically pass paths.EventsDir() which ensures it.
func New(dir string) (*Writer, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("eventlog: stat dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("eventlog: %s is not a directory", dir)
	}
	return &Writer{dir: dir}, nil
}

// Append marshals e (after applying defaults) and writes a single newline-
// terminated JSON line, fsyncing on each append. Concurrency-safe within a
// process.
func (w *Writer) Append(e events.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e = e.WithDefaults()

	if err := w.ensureFileLocked(e.Ts); err != nil {
		return err
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("eventlog: marshal: %w", err)
	}
	// One write call to keep the line atomic under O_APPEND for short events.
	data = append(data, '\n')
	if _, err := w.f.Write(data); err != nil {
		return fmt.Errorf("eventlog: write: %w", err)
	}
	if err := w.f.Sync(); err != nil {
		return fmt.Errorf("eventlog: fsync: %w", err)
	}
	return nil
}

// Close releases the underlying file handle. Safe to call multiple times.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	w.date = ""
	return err
}

func (w *Writer) currentDate(eventTs time.Time) string {
	// Use the event's own timestamp so a midnight-straddling burst still
	// lands in the file matching the wall clock of the event, not whenever
	// the Writer happened to get scheduled.
	if eventTs.IsZero() {
		eventTs = w.nowFn()()
	}
	return eventTs.UTC().Format("2006-01-02")
}

func (w *Writer) nowFn() func() time.Time {
	if w.now != nil {
		return w.now
	}
	return time.Now
}

func (w *Writer) ensureFileLocked(eventTs time.Time) error {
	date := w.currentDate(eventTs)
	if w.f != nil && w.date == date {
		return nil
	}
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
		w.date = ""
	}
	path := filepath.Join(w.dir, date+".jsonl")
	// 0o600 keeps the eventlog readable only to the owner. The parent dir is
	// already 0o700 so this is belt-and-braces — but tarballs, rsync, and
	// accidental `chmod o+x ~` all preserve file modes, so the tight mode
	// matters for any data that leaves this directory in flight.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	w.f = f
	w.date = date
	return nil
}
