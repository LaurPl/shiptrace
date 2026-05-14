package git

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingPollState(t *testing.T) {
	s, err := LoadPRPollState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s == nil || len(s.Seen) != 0 {
		t.Errorf("expected empty state, got %+v", s)
	}
}

func TestPollStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	s := &PRPollState{Seen: map[string]time.Time{
		"https://github.com/foo/bar/pull/1": now,
	}}
	if err := SavePRPollState(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadPRPollState(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Seen["https://github.com/foo/bar/pull/1"]; !ok {
		t.Errorf("key not preserved: %+v", loaded.Seen)
	}
}

func TestFilterNewAndMarkSeen(t *testing.T) {
	state := &PRPollState{Seen: map[string]time.Time{
		"https://github.com/foo/bar/pull/1": time.Now(),
	}}
	prs := []MergedPR{
		{URL: "https://github.com/foo/bar/pull/1", Title: "old"},
		{URL: "https://github.com/foo/bar/pull/2", Title: "new"},
		{URL: "https://github.com/foo/bar/pull/3", Title: "newer"},
	}
	fresh := FilterNew(state, prs)
	if len(fresh) != 2 {
		t.Fatalf("expected 2 fresh, got %d: %+v", len(fresh), fresh)
	}
	MarkSeen(state, fresh)
	if len(state.Seen) != 3 {
		t.Errorf("expected 3 seen after MarkSeen, got %d", len(state.Seen))
	}
}
