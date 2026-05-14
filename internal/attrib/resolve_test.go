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
		SessionID:    id,
		Label:        label,
		StartedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		LastActivity: time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("WriteActive: %v", err)
	}
	return path
}

func TestFlagWinsOverEverything(t *testing.T) {
	t.Setenv(EnvVar, "shp_env")
	ptr := writePointer(t, "shp_ptr", "from pointer")

	r, err := Resolve(Inputs{FlagValue: "shp_flag", GlobalPointerPath: ptr})
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

	r, err := Resolve(Inputs{FlagValue: "shp_same", GlobalPointerPath: ptr})
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

	r, err := Resolve(Inputs{GlobalPointerPath: ptr})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourceEnv || r.SessionID != "shp_env" {
		t.Fatalf("expected env win, got %+v", r)
	}
}

func TestProjectPointerBeatsGlobalPointer(t *testing.T) {
	t.Setenv(EnvVar, "")
	global := writePointer(t, "shp_global", "manual")
	project := writePointer(t, "shp_project", "cc-session")

	r, err := Resolve(Inputs{
		ProjectPointerPath: project,
		GlobalPointerPath:  global,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourceProjectPointer || r.SessionID != "shp_project" {
		t.Fatalf("expected project-pointer win, got %+v", r)
	}
}

func TestGlobalPointerWhenNoProject(t *testing.T) {
	t.Setenv(EnvVar, "")
	ptr := writePointer(t, "shp_global", "manual")

	r, err := Resolve(Inputs{GlobalPointerPath: ptr})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourcePointer || r.SessionID != "shp_global" {
		t.Fatalf("expected pointer win, got %+v", r)
	}
	if r.Label != "manual" {
		t.Fatalf("label not propagated: %q", r.Label)
	}
	if r.StartedAt.IsZero() {
		t.Fatalf("StartedAt not propagated")
	}
}

func TestStaleProjectPointerFallsThrough(t *testing.T) {
	t.Setenv(EnvVar, "")
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	stalePath := filepath.Join(t.TempDir(), "ptr")
	if err := session.WriteActive(stalePath, session.ActivePointer{
		SessionID:    "shp_stale",
		Label:        "old",
		StartedAt:    now.Add(-6 * time.Hour),
		LastActivity: now.Add(-6 * time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Global pointer must be fresh per MaxStaleness or it'd also be skipped.
	globalPath := filepath.Join(t.TempDir(), "global")
	if err := session.WriteActive(globalPath, session.ActivePointer{
		SessionID:    "shp_global",
		Label:        "manual",
		StartedAt:    now.Add(-5 * time.Minute),
		LastActivity: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("seed global: %v", err)
	}

	r, err := Resolve(Inputs{
		ProjectPointerPath: stalePath,
		GlobalPointerPath:  globalPath,
		Now:                now,
		MaxStaleness:       time.Hour,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != SourcePointer {
		t.Fatalf("expected stale project pointer to fall through to global, got %+v", r)
	}
	if r.SessionID != "shp_global" {
		t.Errorf("global session id: %q", r.SessionID)
	}
}

func TestNoneWhenNothingSet(t *testing.T) {
	t.Setenv(EnvVar, "")
	r, err := Resolve(Inputs{GlobalPointerPath: filepath.Join(t.TempDir(), "missing")})
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
