package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

func TestLoadStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := &State{Seen: map[string]int64{
		"/foo/bar.md": 1700000000,
		"/foo/baz.md": 1800000000,
	}}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Seen) != 2 || got.Seen["/foo/bar.md"] != 1700000000 {
		t.Errorf("roundtrip: %+v", got.Seen)
	}
}

func TestLoadStateMissingReturnsEmpty(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s == nil || len(s.Seen) != 0 {
		t.Errorf("expected empty state, got %+v", s)
	}
}

func TestScanDirReturnsAllFilesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "sub/c.md"} {
		full := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	state := &State{Seen: map[string]int64{}}
	matches, err := Scan(state, []string{dir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(matches) != 3 {
		t.Errorf("got %d matches, want 3: %+v", len(matches), matches)
	}
}

func TestScanIgnoresUnchangedFilesOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "x.md")
	_ = os.WriteFile(full, []byte("x"), 0o644)

	state := &State{Seen: map[string]int64{}}
	matches, _ := Scan(state, []string{dir})
	MarkSeen(state, matches)

	matches2, _ := Scan(state, []string{dir})
	if len(matches2) != 0 {
		t.Errorf("expected 0 on second scan, got %+v", matches2)
	}
}

func TestScanPicksUpModifications(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "x.md")
	_ = os.WriteFile(full, []byte("x"), 0o644)

	state := &State{Seen: map[string]int64{}}
	first, _ := Scan(state, []string{dir})
	MarkSeen(state, first)

	// Advance mtime by writing again with explicit future time.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(full, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	matches, _ := Scan(state, []string{dir})
	if len(matches) != 1 {
		t.Errorf("expected 1 modified match, got %+v", matches)
	}
}

func TestScanLiteralFilePath(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "single.md")
	_ = os.WriteFile(full, []byte("x"), 0o644)

	state := &State{Seen: map[string]int64{}}
	matches, err := Scan(state, []string{full})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(matches) != 1 || matches[0].Path != mustAbs(t, full) {
		t.Errorf("matches: %+v", matches)
	}
}

func TestScanGlobPattern(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.txt"} {
		_ = os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644)
	}
	state := &State{Seen: map[string]int64{}}
	matches, err := Scan(state, []string{filepath.Join(dir, "*.md")})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("got %d, want 2: %+v", len(matches), matches)
	}
	// Stable ordering for assert
	paths := []string{matches[0].Path, matches[1].Path}
	sort.Strings(paths)
	if !filepathHasSuffix(paths[0], "a.md") || !filepathHasSuffix(paths[1], "b.md") {
		t.Errorf("unexpected glob expansion: %+v", paths)
	}
}

func TestAttributeFileOverlapWins(t *testing.T) {
	s := newScanStore(t)
	ctx := context.Background()
	seedSessionAndTool(t, s, "shp_a", "/x/file.md", time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC))

	attr, err := AttributeFile(ctx, s, "/x/file.md", time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Attribute: %v", err)
	}
	if attr.Method != MethodFileOverlap || attr.SessionID != "shp_a" {
		t.Errorf("got %+v", attr)
	}
}

func TestAttributeFallsToTimeWindow(t *testing.T) {
	s := newScanStore(t)
	ctx := context.Background()
	// Session ended 5 minutes before the file lands; no tool_events recorded.
	if err := s.UpsertSessionStart(ctx, events.Event{
		EventType: events.SessionStart,
		Ts:        time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		SessionID: "shp_b", Provider: "claude-code", Label: "x",
	}.WithDefaults()); err != nil {
		t.Fatalf("seed start: %v", err)
	}
	if err := s.UpdateSessionStop(ctx, events.Event{
		EventType: events.SessionStop,
		Ts:        time.Date(2026, 5, 14, 10, 25, 0, 0, time.UTC),
		SessionID: "shp_b",
	}.WithDefaults()); err != nil {
		t.Fatalf("seed stop: %v", err)
	}

	attr, err := AttributeFile(ctx, s, "/x/landed.md", time.Date(2026, 5, 14, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Attribute: %v", err)
	}
	if attr.Method != MethodTimeWindow || attr.SessionID != "shp_b" {
		t.Errorf("got %+v", attr)
	}
}

func TestAttributeNoneWhenNothingMatches(t *testing.T) {
	s := newScanStore(t)
	attr, err := AttributeFile(context.Background(), s, "/x/orphan.md", time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Attribute: %v", err)
	}
	if attr.Method != MethodNone {
		t.Errorf("expected none, got %+v", attr)
	}
}

func TestEmitShipEventsAppendsAttributedEvents(t *testing.T) {
	s := newScanStore(t)
	dir := t.TempDir()
	seedSessionAndTool(t, s, "shp_emit", filepath.Join(dir, "x.md"), time.Now().Add(-2*time.Minute))

	full := filepath.Join(dir, "x.md")
	_ = os.WriteFile(full, []byte("x"), 0o644)

	w, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("eventlog: %v", err)
	}
	defer w.Close()

	matches, _ := Scan(&State{Seen: map[string]int64{}}, []string{full})
	n, err := EmitShipEvents(context.Background(), w, s, matches, time.Now().UTC())
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if n != 1 {
		t.Errorf("emitted %d", n)
	}
	// Read back the event
	data, _ := os.ReadFile(filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	if len(data) == 0 {
		t.Fatalf("no event written")
	}
	var ev events.Event
	_ = json.Unmarshal(splitFirstLine(data), &ev)
	if ev.EventType != events.Ship {
		t.Errorf("event_type: %q", ev.EventType)
	}
	if ev.Metadata["kind"] != "file_landed" {
		t.Errorf("kind: %v", ev.Metadata["kind"])
	}
	if ev.SessionID != "shp_emit" {
		t.Errorf("session: %q", ev.SessionID)
	}
	if ev.Metadata["attribution_method"] != string(MethodFileOverlap) {
		t.Errorf("attribution_method: %v", ev.Metadata["attribution_method"])
	}
}

func newScanStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedSessionAndTool(t *testing.T, s *store.Store, id, file string, ts time.Time) {
	t.Helper()
	ctx := context.Background()
	if err := s.UpsertSessionStart(ctx, events.Event{
		EventType: events.SessionStart, Ts: ts, SessionID: id, Provider: "claude-code", Label: "x",
	}.WithDefaults()); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	abs, _ := filepath.Abs(file)
	if err := s.InsertToolEvent(ctx, events.Event{
		EventType:    events.ToolUse,
		Ts:           ts,
		SessionID:    id,
		Tool:         "Edit",
		FilesTouched: []string{abs},
	}.WithDefaults()); err != nil {
		t.Fatalf("seed tool: %v", err)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func splitFirstLine(b []byte) []byte {
	for i, c := range b {
		if c == '\n' {
			return b[:i]
		}
	}
	return b
}

func filepathHasSuffix(p, suffix string) bool {
	return filepath.Base(p) == suffix
}
