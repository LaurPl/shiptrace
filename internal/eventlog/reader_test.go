package eventlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LaurPl/shiptrace/internal/events"
)

func writeJSONL(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
}

func TestScanFileFromZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	writeJSONL(t, path, []string{
		`{"schema_version":"1","event_type":"session_start","ts":"2026-05-14T10:00:00Z","session_id":"shp_a"}`,
		`{"schema_version":"1","event_type":"ship","ts":"2026-05-14T10:00:01Z","session_id":"shp_a"}`,
		`{"schema_version":"1","event_type":"session_stop","ts":"2026-05-14T10:00:02Z","session_id":"shp_a"}`,
	})

	var seen []events.EventType
	final, err := ScanFile(path, 0, func(e events.Event, nextOffset int64) error {
		seen = append(seen, e.EventType)
		return nil
	})
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}

	want := []events.EventType{events.SessionStart, events.Ship, events.SessionStop}
	if len(seen) != len(want) {
		t.Fatalf("got %d events, want %d", len(seen), len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("event %d: got %q want %q", i, seen[i], want[i])
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if final != info.Size() {
		t.Fatalf("final offset: got %d, want %d (file size)", final, info.Size())
	}
}

func TestScanFileResumesFromOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	writeJSONL(t, path, []string{
		`{"schema_version":"1","event_type":"session_start","ts":"2026-05-14T10:00:00Z","session_id":"shp_a"}`,
		`{"schema_version":"1","event_type":"ship","ts":"2026-05-14T10:00:01Z","session_id":"shp_a"}`,
	})

	// First pass — consume everything.
	final, err := ScanFile(path, 0, func(e events.Event, n int64) error { return nil })
	if err != nil {
		t.Fatalf("first ScanFile: %v", err)
	}

	// Re-scan from the final offset: zero new events.
	called := 0
	final2, err := ScanFile(path, final, func(e events.Event, n int64) error {
		called++
		return nil
	})
	if err != nil {
		t.Fatalf("resume ScanFile: %v", err)
	}
	if called != 0 {
		t.Fatalf("expected 0 callbacks on resume, got %d", called)
	}
	if final2 != final {
		t.Fatalf("offset advanced unexpectedly: %d -> %d", final, final2)
	}
}

func TestScanFileIgnoresPartialTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// One complete line, one partial.
	_, _ = f.WriteString(`{"schema_version":"1","event_type":"ship","ts":"2026-05-14T10:00:00Z"}` + "\n")
	_, _ = f.WriteString(`{"schema_version":"1","event_type":"sh`) // partial
	_ = f.Close()

	count := 0
	var observedNext int64
	_, err = ScanFile(path, 0, func(e events.Event, n int64) error {
		count++
		observedNext = n
		return nil
	})
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 complete event, got %d", count)
	}
	if observedNext == 0 {
		t.Fatalf("nextOffset should be the byte after the complete line")
	}
}

func TestScanFileMissingFileErrors(t *testing.T) {
	_, err := ScanFile(filepath.Join(t.TempDir(), "missing.jsonl"), 0, func(events.Event, int64) error { return nil })
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}
