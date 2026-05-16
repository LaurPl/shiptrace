package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	wantTables := []string{"sessions", "tool_events", "ship_events", "schema_migrations"}
	for _, table := range wantTables {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}

	wantIndexes := []string{
		"idx_tool_events_session",
		"idx_ship_events_session",
		"idx_sessions_project_time",
		"idx_sessions_provider_lookup",
	}
	for _, idx := range wantIndexes {
		var name string
		err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name)
		if err != nil {
			t.Errorf("index %s missing: %v", idx, err)
		}
	}
}

func TestOpenIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		_ = s.Close()
	}
}

// TestOpenWithUrlSpecialCharsInPath confirms that paths containing URL meta
// characters (?, #, &, %) don't break the DSN or, worse, let extra pragmas
// be smuggled into modernc.org/sqlite via the path itself. We assert by
// reading back a pragma value: if path-injection were possible, an attacker
// could flip foreign_keys off via "?_pragma=foreign_keys(OFF)" embedded in
// the home dir.
func TestOpenWithUrlSpecialCharsInPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "weird?dir#with&meta%chars")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Skipf("filesystem rejected dir name: %v", err)
	}
	path := filepath.Join(dir, "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var fk int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1 (path-injection may have disabled it)", fk)
	}
}
