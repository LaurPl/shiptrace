// Package session manages the active-session pointer file used by the
// manual recorder. The pointer lives at <home>/.current-session and exists
// so that `shiptrace ship` and `shiptrace session stop` can find the session
// the user started in the same shell session.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ActivePointer is the small JSON record we persist between commands.
type ActivePointer struct {
	SessionID string    `json:"session_id"`
	Label     string    `json:"label"`
	StartedAt time.Time `json:"started_at"`
}

// WriteActive writes p to path atomically (write-temp-then-rename) so a
// crashed write doesn't leave a half-parsed file that breaks subsequent
// reads.
func WriteActive(path string, p ActivePointer) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("session: marshal pointer: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("session: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("session: rename: %w", err)
	}
	return nil
}

// ReadActive returns the active pointer if one exists. A missing file is
// signaled by (nil, nil) — that's the common case, not an error.
func ReadActive(path string) (*ActivePointer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("session: read pointer: %w", err)
	}
	var p ActivePointer
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("session: parse pointer at %s: %w", filepath.Base(path), err)
	}
	return &p, nil
}

// ClearActive removes the pointer file. Missing is not an error.
func ClearActive(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("session: remove pointer: %w", err)
	}
	return nil
}
