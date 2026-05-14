package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/store"
)

// Provider is the value stamped on ship events emitted by this adapter.
const Provider = "filesystem"

// StateFileName is the dedupe state file under SHIPTRACE_HOME. We track
// (path, mtime_unix) pairs so the same file modified once doesn't fire
// repeatedly until it's modified again.
const StateFileName = ".fs-state.json"

// State maps absolute file path to the last-seen mtime (unix seconds).
type State struct {
	Seen map[string]int64 `json:"seen"`
}

// LoadState reads the state file at path; missing → empty state.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{Seen: map[string]int64{}}, nil
		}
		return nil, fmt.Errorf("filesystem: read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("filesystem: parse state: %w", err)
	}
	if s.Seen == nil {
		s.Seen = map[string]int64{}
	}
	return &s, nil
}

// SaveState writes path atomically.
func SaveState(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Match is one file that the scanner picked up. The scanner stops at
// match emission; emitting ship events happens after attribution.
type Match struct {
	Path  string
	Mtime time.Time
}

// Scan expands shipPaths (each may contain glob wildcards), walks them
// recursively, and returns every file whose mtime is newer than what
// state records. Symlinks are followed; directories themselves are not
// emitted.
//
// Globs use the doublestar pattern via filepath.Glob (so `**` is not
// supported by stdlib). For v0.1 we accept stdlib semantics: the user
// should provide concrete directory paths or shallow globs like
// `/x/published/*`, not `/x/**/published/*`. Day-5+ may upgrade.
func Scan(state *State, shipPaths []string) ([]Match, error) {
	if state == nil {
		return nil, errors.New("filesystem: nil state")
	}
	var out []Match
	seenInRun := map[string]struct{}{}
	for _, pattern := range shipPaths {
		matches, err := expandPattern(pattern)
		if err != nil {
			return nil, fmt.Errorf("filesystem: expand %q: %w", pattern, err)
		}
		for _, p := range matches {
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			if _, dup := seenInRun[abs]; dup {
				continue
			}
			seenInRun[abs] = struct{}{}
			info, err := os.Stat(abs)
			if err != nil {
				continue
			}
			if info.IsDir() {
				if err := walkDir(abs, info, state, seenInRun, &out); err != nil {
					return nil, err
				}
				continue
			}
			if isNewer(state, abs, info.ModTime()) {
				out = append(out, Match{Path: abs, Mtime: info.ModTime()})
			}
		}
	}
	return out, nil
}

// expandPattern returns the literal path when no glob meta-character is
// present, otherwise the stdlib Glob expansion.
func expandPattern(pattern string) ([]string, error) {
	if !containsGlobMeta(pattern) {
		return []string{pattern}, nil
	}
	return filepath.Glob(pattern)
}

func containsGlobMeta(s string) bool {
	for _, ch := range s {
		switch ch {
		case '*', '?', '[':
			return true
		}
	}
	return false
}

func walkDir(root string, _ os.FileInfo, state *State, seenInRun map[string]struct{}, out *[]Match) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // best-effort: a permission denied on one subtree shouldn't kill the whole scan
		}
		if info.IsDir() {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		if _, dup := seenInRun[abs]; dup {
			return nil
		}
		seenInRun[abs] = struct{}{}
		if isNewer(state, abs, info.ModTime()) {
			*out = append(*out, Match{Path: abs, Mtime: info.ModTime()})
		}
		return nil
	})
}

func isNewer(state *State, abs string, mtime time.Time) bool {
	prev, ok := state.Seen[abs]
	if !ok {
		return true
	}
	return mtime.Unix() > prev
}

// MarkSeen records the matches in state at their observed mtimes. Caller
// is expected to SaveState after MarkSeen + EmitShipEvents succeed.
func MarkSeen(state *State, matches []Match) {
	if state.Seen == nil {
		state.Seen = map[string]int64{}
	}
	for _, m := range matches {
		state.Seen[m.Path] = m.Mtime.Unix()
	}
}

// EmitShipEvents writes one ship event per match, attributing each via
// AttributeFile. Returns the count emitted.
func EmitShipEvents(ctx context.Context, w *eventlog.Writer, s *store.Store, matches []Match, now time.Time) (int, error) {
	for _, m := range matches {
		attr, err := AttributeFile(ctx, s, m.Path, m.Mtime)
		if err != nil {
			return 0, err
		}
		meta := map[string]any{
			"kind":        "file_landed",
			"ref":         m.Path,
			"mtime":       m.Mtime.UTC().Format(time.RFC3339),
			"file_landed": m.Path,
		}
		if attr.Method != MethodNone {
			meta["attribution_method"] = string(attr.Method)
		}
		ev := events.Event{
			EventType:    events.Ship,
			Ts:           now,
			SessionID:    attr.SessionID,
			Provider:     Provider,
			FilesTouched: []string{m.Path},
			Metadata:     meta,
		}
		if err := w.Append(ev); err != nil {
			return 0, err
		}
	}
	return len(matches), nil
}
