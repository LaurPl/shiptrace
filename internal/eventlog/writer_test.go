package eventlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
)

func TestAppendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	want := []events.Event{
		{EventType: events.SessionStart, SessionID: "shp_aaaaaaaaaaaa", Provider: "manual", Label: "first"},
		{EventType: events.Ship, SessionID: "shp_aaaaaaaaaaaa", Metadata: map[string]any{"kind": "manual"}},
		{EventType: events.SessionStop, SessionID: "shp_aaaaaaaaaaaa"},
	}
	for _, e := range want {
		if err := w.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	today := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, today+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	scanner := bufio.NewScanner(f)
	var got []events.Event
	for scanner.Scan() {
		var e events.Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("parse: %v", err)
		}
		got = append(got, e)
	}
	if scanner.Err() != nil {
		t.Fatalf("scan: %v", scanner.Err())
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].EventType != want[i].EventType {
			t.Errorf("event %d type: got %q want %q", i, got[i].EventType, want[i].EventType)
		}
		if got[i].SchemaVersion != events.SchemaVersion {
			t.Errorf("event %d missing schema_version", i)
		}
		if got[i].Ts.IsZero() {
			t.Errorf("event %d missing ts", i)
		}
	}
}

func TestRolloverWhenEventDateChanges(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	day1 := time.Date(2026, 5, 14, 23, 59, 59, 0, time.UTC)
	day2 := time.Date(2026, 5, 15, 0, 0, 1, 0, time.UTC)

	if err := w.Append(events.Event{EventType: events.SessionStart, Ts: day1, SessionID: "shp_a"}); err != nil {
		t.Fatalf("append day1: %v", err)
	}
	if err := w.Append(events.Event{EventType: events.SessionStop, Ts: day2, SessionID: "shp_a"}); err != nil {
		t.Fatalf("append day2: %v", err)
	}

	for _, name := range []string{"2026-05-14.jsonl", "2026-05-15.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}
}

func TestAppendErrorsWhenDirMissing(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatalf("expected error for missing dir")
	}
}

func TestCloseIdempotent(t *testing.T) {
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}
