package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionMapRoundTrip(t *testing.T) {
	home := t.TempDir()
	m, err := NewSessionMap(home)
	if err != nil {
		t.Fatalf("NewSessionMap: %v", err)
	}

	if err := m.Set("cc-abc-123", "shp_xyz789abcde"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := m.Get("cc-abc-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "shp_xyz789abcde" {
		t.Errorf("Get: %q", got)
	}

	if err := m.Delete("cc-abc-123"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got2, err := m.Get("cc-abc-123")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got2 != "" {
		t.Errorf("expected empty after delete, got %q", got2)
	}
}

func TestSessionMapGetMissingReturnsEmpty(t *testing.T) {
	m, _ := NewSessionMap(t.TempDir())
	got, err := m.Get("never-set")
	if err != nil {
		t.Fatalf("err on missing: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestSessionMapRejectsPathTraversal(t *testing.T) {
	m, _ := NewSessionMap(t.TempDir())
	for _, bad := range []string{
		"",
		".",
		"..",
		"../escape",
		"a/b",
		"..\\win",
		"..\\..\\etc",
		"/absolute",
		"sub/../escape",
		"contains\x00nul",
	} {
		if err := m.Set(bad, "shp_x"); err == nil {
			t.Errorf("Set should reject %q", bad)
		}
		if _, err := m.Get(bad); err == nil {
			t.Errorf("Get should reject %q", bad)
		}
	}
}

func TestSessionMapAcceptsRealisticUUIDs(t *testing.T) {
	m, _ := NewSessionMap(t.TempDir())
	// UUIDs and the kind of strings the dev style guide shows in CC payloads.
	for _, good := range []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"abc.123",          // dots are fine when they don't compose ".."
		"some_session_id",
		"aaa..bbb",         // double-dot mid-token used to over-reject; now passes
	} {
		if err := m.Set(good, "shp_test"); err != nil {
			t.Errorf("Set rejected valid id %q: %v", good, err)
		}
	}
}

func TestSessionMapDeleteMissingIsNotAnError(t *testing.T) {
	m, _ := NewSessionMap(t.TempDir())
	if err := m.Delete("never-set"); err != nil {
		t.Fatalf("Delete missing: %v", err)
	}
}

func TestSessionMapPersistsAcrossInstances(t *testing.T) {
	home := t.TempDir()
	m1, _ := NewSessionMap(home)
	if err := m1.Set("cc-persist", "shp_first"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	m2, _ := NewSessionMap(home)
	got, _ := m2.Get("cc-persist")
	if got != "shp_first" {
		t.Errorf("not persisted: %q", got)
	}
}

func TestSessionMapDirCreated(t *testing.T) {
	home := t.TempDir()
	if _, err := NewSessionMap(home); err != nil {
		t.Fatalf("NewSessionMap: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, CCSessionsDirName))
	if err != nil || !info.IsDir() {
		t.Fatalf("dir not created: err=%v info=%v", err, info)
	}
}
