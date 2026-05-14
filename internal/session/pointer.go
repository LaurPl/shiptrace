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
//
// LastActivity is bumped on every recorder event so the git post-commit
// adapter can tell a session that ended cleanly from one whose process
// crashed hours ago. A pointer is considered stale when
// now - LastActivity > maxAge (see IsStale).
type ActivePointer struct {
	SessionID    string    `json:"session_id"`
	Label        string    `json:"label"`
	StartedAt    time.Time `json:"started_at"`
	LastActivity time.Time `json:"last_activity,omitempty"`
}

// DefaultMaxStaleness is the default age beyond which a pointer is
// considered abandoned. Four hours is a deliberate "long enough to cover
// lunch, short enough that a forgotten manual session doesn't attribute
// tomorrow's commits."
const DefaultMaxStaleness = 4 * time.Hour

// IsStale returns true when LastActivity (or StartedAt if LastActivity is
// unset) is older than maxAge relative to now.
func (p *ActivePointer) IsStale(now time.Time, maxAge time.Duration) bool {
	if p == nil {
		return false
	}
	ref := p.LastActivity
	if ref.IsZero() {
		ref = p.StartedAt
	}
	if ref.IsZero() {
		return false
	}
	return now.Sub(ref) > maxAge
}

// WriteActive writes p to path atomically (write-temp-then-rename) so a
// crashed write doesn't leave a half-parsed file that breaks subsequent
// reads. The parent dir is created if missing.
func WriteActive(path string, p ActivePointer) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("session: mkdir pointer dir: %w", err)
	}
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

// Touch bumps LastActivity on the pointer at path, leaving every other
// field intact. A missing pointer is a no-op (no error). Used by every
// CC hook to keep stale-detection accurate.
func Touch(path string, now time.Time) error {
	p, err := ReadActive(path)
	if err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	p.LastActivity = now
	return WriteActive(path, *p)
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
