// Package paths resolves the on-disk locations shiptrace uses: home,
// events dir, SQLite DB, pointer file, ingester checkpoint. Honors
// $SHIPTRACE_HOME for testability and for non-standard installs.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HomeEnv is the env var that overrides the default ~/.shiptrace location.
const HomeEnv = "SHIPTRACE_HOME"

// Home returns the shiptrace home dir, creating it with 0700 if missing.
func Home() (string, error) {
	if override := os.Getenv(HomeEnv); override != "" {
		return ensureDir(override, 0o700)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("paths: resolve user home: %w", err)
	}
	return ensureDir(filepath.Join(userHome, ".shiptrace"), 0o700)
}

// EventsDir returns <home>/events, creating it with 0700 if missing.
func EventsDir() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return ensureDir(filepath.Join(h, "events"), 0o700)
}

// DBPath returns <home>/shiptrace.db. The file is not created here; the
// store opens (and creates) it.
func DBPath() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "shiptrace.db"), nil
}

// PointerPath returns the path to the manual recorder's active-session marker.
func PointerPath() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".current-session"), nil
}

// CheckpointPath returns the path to the ingester's per-file offset
// checkpoint JSON.
func CheckpointPath() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".ingest-checkpoint.json"), nil
}

func ensureDir(path string, mode os.FileMode) (string, error) {
	if err := os.MkdirAll(path, mode); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("paths: mkdir %s: %w", path, err)
		}
	}
	return path, nil
}
