package claudecode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CCSessionsDirName is the subdir under SHIPTRACE_HOME where we persist
// per-CC-session shp_ id mappings. One tiny file per session keeps reads
// O(1) without parsing a global JSON; cleanup on Stop hook removes the file.
const CCSessionsDirName = "cc-sessions"

// SessionMap persists the mapping <CC session UUID> → <shp_ session id>.
// One file per CC session, named after the UUID, containing only the shp_
// ID. Reads and writes are O(1) without parsing a global JSON file, which
// matters for the hook's hot path.
type SessionMap struct {
	dir string
}

// NewSessionMap constructs a SessionMap rooted at home/cc-sessions/, creating
// the dir if missing.
func NewSessionMap(home string) (*SessionMap, error) {
	dir := filepath.Join(home, CCSessionsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("claudecode: mkdir cc-sessions: %w", err)
	}
	return &SessionMap{dir: dir}, nil
}

// Set records mapping ccID → shpID atomically (tmp + rename).
func (m *SessionMap) Set(ccID, shpID string) error {
	if err := validateCCID(ccID); err != nil {
		return err
	}
	path := filepath.Join(m.dir, ccID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(shpID), 0o600); err != nil {
		return fmt.Errorf("claudecode: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("claudecode: rename: %w", err)
	}
	return nil
}

// Get returns the shp_ ID for ccID, or ("", nil) if no mapping exists.
// Distinguishing missing from error matters: a missing mapping on
// PostToolUse usually means SessionStart didn't fire (rare but documented
// in CC) and we want to recover, not crash.
func (m *SessionMap) Get(ccID string) (string, error) {
	if err := validateCCID(ccID); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(m.dir, ccID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("claudecode: read mapping: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// Delete removes the mapping (called on Stop). Missing is not an error.
func (m *SessionMap) Delete(ccID string) error {
	if err := validateCCID(ccID); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(m.dir, ccID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("claudecode: delete mapping: %w", err)
	}
	return nil
}

// validateCCID guards against path traversal. A CC session id is used as a
// single filename under cc-sessions/, so we require it to contain no path
// separators (either OS), no NUL, and to be cross-OS local per
// filepath.IsLocal (which rejects "..", absolute paths, and Windows reserved
// names). IsLocal alone is too permissive because on Unix it lets "/" and
// "\\" through as filename characters; the explicit separator check closes
// that gap.
func validateCCID(ccID string) error {
	if ccID == "" {
		return errors.New("claudecode: empty cc session id")
	}
	if strings.ContainsRune(ccID, 0) {
		return fmt.Errorf("claudecode: invalid cc session id (NUL byte)")
	}
	if strings.ContainsAny(ccID, `/\`) {
		return fmt.Errorf("claudecode: invalid cc session id (path separator): %q", ccID)
	}
	if !filepath.IsLocal(ccID) {
		return fmt.Errorf("claudecode: invalid cc session id: %q", ccID)
	}
	return nil
}
