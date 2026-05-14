package ingest

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmptyMap(t *testing.T) {
	c, err := LoadCheckpoints(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c) != 0 {
		t.Fatalf("expected empty map, got %v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ck.json")
	want := Checkpoints{
		"2026-05-14.jsonl": 1024,
		"2026-05-15.jsonl": 0,
	}
	if err := SaveCheckpoints(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := LoadCheckpoints(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("offset for %s: got %d want %d", k, got[k], v)
		}
	}
}
