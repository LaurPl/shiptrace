package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadClearRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ptr")
	want := ActivePointer{
		SessionID:    "shp_abc123def456",
		Label:        "writing slides",
		StartedAt:    time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
		LastActivity: time.Date(2026, 5, 14, 10, 5, 0, 0, time.UTC),
	}
	if err := WriteActive(path, want); err != nil {
		t.Fatalf("WriteActive: %v", err)
	}
	got, err := ReadActive(path)
	if err != nil {
		t.Fatalf("ReadActive: %v", err)
	}
	if got == nil {
		t.Fatalf("ReadActive returned nil after write")
	}
	if got.SessionID != want.SessionID || got.Label != want.Label || !got.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", *got, want)
	}
	if !got.LastActivity.Equal(want.LastActivity) {
		t.Fatalf("LastActivity not preserved: %v", got.LastActivity)
	}

	if err := ClearActive(path); err != nil {
		t.Fatalf("ClearActive: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should be gone after Clear: %v", err)
	}
}

func TestReadActiveMissingReturnsNilNil(t *testing.T) {
	p, err := ReadActive(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("err on missing file: %v", err)
	}
	if p != nil {
		t.Fatalf("expected nil pointer, got %+v", *p)
	}
}

func TestClearActiveMissingNotAnError(t *testing.T) {
	if err := ClearActive(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Fatalf("clearing missing file errored: %v", err)
	}
}

func TestWriteActiveCreatesParentDir(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "c", "pointer.json")
	if err := WriteActive(deep, ActivePointer{SessionID: "shp_x"}); err != nil {
		t.Fatalf("WriteActive into missing dirs: %v", err)
	}
	if _, err := os.Stat(deep); err != nil {
		t.Fatalf("pointer not written: %v", err)
	}
}

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		p         *ActivePointer
		max       time.Duration
		wantStale bool
	}{
		{"nil pointer", nil, time.Hour, false},
		{"empty timestamps", &ActivePointer{}, time.Hour, false},
		{"fresh activity", &ActivePointer{LastActivity: now.Add(-5 * time.Minute)}, time.Hour, false},
		{"old activity", &ActivePointer{LastActivity: now.Add(-2 * time.Hour)}, time.Hour, true},
		{"falls back to StartedAt", &ActivePointer{StartedAt: now.Add(-2 * time.Hour)}, time.Hour, true},
		{"LastActivity wins over old StartedAt", &ActivePointer{StartedAt: now.Add(-10 * time.Hour), LastActivity: now.Add(-1 * time.Minute)}, time.Hour, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.p.IsStale(now, c.max)
			if got != c.wantStale {
				t.Errorf("IsStale: got %v, want %v", got, c.wantStale)
			}
		})
	}
}

func TestTouchBumpsLastActivity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ptr")
	t0 := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	if err := WriteActive(path, ActivePointer{SessionID: "shp_t", StartedAt: t0, LastActivity: t0}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	t1 := t0.Add(30 * time.Minute)
	if err := Touch(path, t1); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ := ReadActive(path)
	if !got.LastActivity.Equal(t1) {
		t.Errorf("Touch did not bump: %v", got.LastActivity)
	}
	if !got.StartedAt.Equal(t0) {
		t.Errorf("Touch corrupted StartedAt: %v", got.StartedAt)
	}
}

func TestTouchOnMissingIsNoop(t *testing.T) {
	if err := Touch(filepath.Join(t.TempDir(), "nope"), time.Now()); err != nil {
		t.Fatalf("Touch on missing pointer should be no-op, got %v", err)
	}
}
