// Package ingest is the only package allowed to import both eventlog (the
// JSONL source of truth) and store (the SQLite materialized view). That
// asymmetry enforces "JSONL is canonical, SQLite is derived" across the
// rest of the codebase.
package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Checkpoints map basename -> last-ingested byte offset. The basename
// (rather than full path) keeps the file portable across SHIPTRACE_HOME
// relocations.
type Checkpoints map[string]int64

// LoadCheckpoints returns the saved offsets, or an empty map if the file
// does not exist yet.
func LoadCheckpoints(path string) (Checkpoints, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Checkpoints{}, nil
		}
		return nil, fmt.Errorf("ingest: read checkpoint: %w", err)
	}
	c := Checkpoints{}
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("ingest: parse checkpoint: %w", err)
	}
	return c, nil
}

// SaveCheckpoints writes the map atomically (tmp + rename) so a crashed
// write does not leave a half-parsed file.
func SaveCheckpoints(path string, c Checkpoints) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("ingest: marshal checkpoint: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("ingest: write tmp checkpoint: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ingest: rename checkpoint: %w", err)
	}
	return nil
}
