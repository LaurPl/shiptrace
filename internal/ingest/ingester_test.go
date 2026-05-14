package ingest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

type ingestFixture struct {
	t         *testing.T
	store     *store.Store
	eventsDir string
	cp        string
	writer    *eventlog.Writer
	ingester  *Ingester
}

func newFixture(t *testing.T) *ingestFixture {
	t.Helper()
	home := t.TempDir()
	eventsDir := filepath.Join(home, "events")
	if err := os.Mkdir(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	dbPath := filepath.Join(home, "shiptrace.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	w, err := eventlog.New(eventsDir)
	if err != nil {
		t.Fatalf("eventlog.New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	cp := filepath.Join(home, ".ingest-checkpoint.json")
	ing := New(s, eventsDir, cp)
	return &ingestFixture{t: t, store: s, eventsDir: eventsDir, cp: cp, writer: w, ingester: ing}
}

func (f *ingestFixture) append(e events.Event) {
	f.t.Helper()
	if err := f.writer.Append(e); err != nil {
		f.t.Fatalf("append: %v", err)
	}
}

func (f *ingestFixture) countRows(table string) int {
	f.t.Helper()
	var n int
	if err := f.store.DB().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		f.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestIngestOnceProcessesAllEventTypes(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()

	f.append(events.Event{EventType: events.SessionStart, Ts: now, SessionID: "shp_a", Provider: "manual", Label: "test"})
	f.append(events.Event{EventType: events.Ship, Ts: now.Add(time.Second), SessionID: "shp_a", Metadata: map[string]any{"kind": "manual", "attribution_method": "explicit"}})
	f.append(events.Event{EventType: events.SessionStop, Ts: now.Add(2 * time.Second), SessionID: "shp_a"})

	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("IngestOnce: %v", err)
	}

	if got := f.countRows("sessions"); got != 1 {
		t.Errorf("sessions: got %d want 1", got)
	}
	if got := f.countRows("ship_events"); got != 1 {
		t.Errorf("ship_events: got %d want 1", got)
	}

	var endTs sql.NullInt64
	if err := f.store.DB().QueryRow("SELECT end_ts FROM sessions WHERE id = 'shp_a'").Scan(&endTs); err != nil {
		t.Fatalf("query end_ts: %v", err)
	}
	if !endTs.Valid {
		t.Errorf("end_ts should be set after session_stop")
	}
}

func TestIngestOnceIsIncrementalViaCheckpoint(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()

	f.append(events.Event{EventType: events.SessionStart, Ts: now, SessionID: "shp_b", Provider: "manual", Label: "x"})
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("first IngestOnce: %v", err)
	}
	if got := f.countRows("ship_events"); got != 0 {
		t.Errorf("ship_events after start-only: got %d want 0", got)
	}

	f.append(events.Event{EventType: events.Ship, Ts: now.Add(time.Second), SessionID: "shp_b", Metadata: map[string]any{"kind": "manual"}})
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("second IngestOnce: %v", err)
	}
	if got := f.countRows("ship_events"); got != 1 {
		t.Errorf("ship_events after ship: got %d want 1", got)
	}

	// Run again with no new events — counts should be unchanged.
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("third IngestOnce: %v", err)
	}
	if got := f.countRows("ship_events"); got != 1 {
		t.Errorf("ship_events after no-op pass: got %d want 1", got)
	}
}

func TestIngestOnceCheckpointPersists(t *testing.T) {
	f := newFixture(t)
	now := time.Now().UTC()
	f.append(events.Event{EventType: events.SessionStart, Ts: now, SessionID: "shp_c", Provider: "manual", Label: "x"})
	if err := f.ingester.IngestOnce(context.Background()); err != nil {
		t.Fatalf("IngestOnce: %v", err)
	}
	c, err := LoadCheckpoints(f.cp)
	if err != nil {
		t.Fatalf("LoadCheckpoints: %v", err)
	}
	if len(c) != 1 {
		t.Fatalf("expected 1 checkpoint entry, got %d", len(c))
	}
	for name, off := range c {
		if off <= 0 {
			t.Errorf("checkpoint for %s should be > 0, got %d", name, off)
		}
	}
}
