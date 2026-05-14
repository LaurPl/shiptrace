package attrib

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/session"
)

func writePointer(t *testing.T, id, label string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ptr")
	if err := session.WriteActive(path, session.ActivePointer{
		SessionID: id,
		Label:     label,
		StartedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("WriteActive: %v", err)
	}
	return path
}

func TestFlagWinsOverEverything(t *testing.T) {
	t.Setenv(EnvVar, "shp_env")
	ptr := writePointer(t, "shp_ptr", "from pointer")

	r, err := Resolve("shp_flag", ptr)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourceFlag || r.SessionID != "shp_flag" {
		t.Fatalf("expected flag win, got %+v", r)
	}
	if r.Conflict == nil || r.Conflict.LosingSource != SourceEnv || r.Conflict.LosingSessionID != "shp_env" {
		t.Fatalf("expected env conflict, got %+v", r.Conflict)
	}
}

func TestFlagWithoutConflictWhenEnvMatches(t *testing.T) {
	t.Setenv(EnvVar, "shp_same")
	ptr := writePointer(t, "shp_ptr", "x")

	r, err := Resolve("shp_same", ptr)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Conflict != nil {
		t.Fatalf("expected no conflict when flag == env, got %+v", r.Conflict)
	}
}

func TestEnvWinsOverPointer(t *testing.T) {
	t.Setenv(EnvVar, "shp_env")
	ptr := writePointer(t, "shp_ptr", "x")

	r, err := Resolve("", ptr)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourceEnv || r.SessionID != "shp_env" {
		t.Fatalf("expected env win, got %+v", r)
	}
}

func TestPointerWhenNoFlagOrEnv(t *testing.T) {
	t.Setenv(EnvVar, "")
	ptr := writePointer(t, "shp_ptr", "from pointer")

	r, err := Resolve("", ptr)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourcePointer || r.SessionID != "shp_ptr" {
		t.Fatalf("expected pointer win, got %+v", r)
	}
	if r.Label != "from pointer" {
		t.Fatalf("label not propagated: %q", r.Label)
	}
	if r.StartedAt.IsZero() {
		t.Fatalf("StartedAt not propagated")
	}
}

func TestNoneWhenNothingSet(t *testing.T) {
	t.Setenv(EnvVar, "")
	r, err := Resolve("", filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourceNone {
		t.Fatalf("expected none, got %+v", r)
	}
	if r.SessionID != "" {
		t.Fatalf("SessionID should be empty: %q", r.SessionID)
	}
}
