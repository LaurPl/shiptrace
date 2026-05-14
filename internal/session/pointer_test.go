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
		SessionID: "shp_abc123def456",
		Label:     "writing slides",
		StartedAt: time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC),
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
