// Package ccsettings reads, merges, and writes Claude Code's
// ~/.claude/settings.json so `shiptrace init` can install the five hooks
// shiptrace relies on without clobbering the user's existing config.
//
// The merge strategy is conservative: we APPEND a shiptrace command entry
// to each matching hook event, NEVER overwrite the array or replace any
// existing entry. Idempotency is provided by detecting an already-installed
// shiptrace-cc-hook command and short-circuiting.
package ccsettings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HookEventName names a Claude Code hook event. We mirror the names CC uses
// in settings.json so a quick diff against CC's docs is grep-friendly.
type HookEventName string

const (
	SessionStart     HookEventName = "SessionStart"
	UserPromptSubmit HookEventName = "UserPromptSubmit"
	PostToolUse      HookEventName = "PostToolUse"
	SubagentStop     HookEventName = "SubagentStop"
	Stop             HookEventName = "Stop"
)

// ShiptraceHooks is the set of (CC event, shiptrace-cc-hook subcommand) we
// install. Source of truth — both init and doctor read from this slice.
var ShiptraceHooks = []struct {
	Event      HookEventName
	Subcommand string
	Matcher    string // empty for events that have no matcher field
}{
	{SessionStart, "session-start", ""},
	{UserPromptSubmit, "prompt", ""},
	{PostToolUse, "tool-use", "*"},
	{SubagentStop, "subagent-stop", ""},
	{Stop, "stop", ""},
}

// Settings represents the subset of ~/.claude/settings.json we touch. We
// preserve unknown top-level keys via Extras so writing back doesn't
// strip the user's other config.
type Settings struct {
	Hooks  map[HookEventName][]HookGroup `json:"hooks,omitempty"`
	Extras map[string]json.RawMessage    `json:"-"`
}

// HookGroup is one entry in the array under a hook event. CC accepts an
// optional "matcher" string and an array of "hooks" each carrying a type
// + command.
type HookGroup struct {
	Matcher string `json:"matcher,omitempty"`
	Hooks   []Hook `json:"hooks"`
}

// Hook is one command entry.
type Hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// Read parses settings.json from path. A missing file returns an empty
// Settings — that's the common "first run" case, not an error.
func Read(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Settings{}, nil
		}
		return nil, fmt.Errorf("ccsettings: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return &Settings{}, nil
	}
	var extras map[string]json.RawMessage
	if err := json.Unmarshal(data, &extras); err != nil {
		return nil, fmt.Errorf("ccsettings: parse extras: %w", err)
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("ccsettings: parse: %w", err)
	}
	delete(extras, "hooks")
	if len(extras) > 0 {
		s.Extras = extras
	}
	return &s, nil
}

// Write serializes s to path atomically (tmp + rename) so a crashed write
// can't leave a half-written settings.json that breaks Claude Code.
func Write(path string, s *Settings) error {
	combined := map[string]json.RawMessage{}
	for k, v := range s.Extras {
		combined[k] = v
	}
	if len(s.Hooks) > 0 {
		hb, err := json.Marshal(s.Hooks)
		if err != nil {
			return fmt.Errorf("ccsettings: marshal hooks: %w", err)
		}
		combined["hooks"] = hb
	}
	out, err := json.MarshalIndent(combined, "", "  ")
	if err != nil {
		return fmt.Errorf("ccsettings: marshal: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("ccsettings: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("ccsettings: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ccsettings: rename: %w", err)
	}
	return nil
}

// MergeShiptraceHooks adds shiptrace's hook entries to s if not already
// present. The shiptraceBin path is the absolute path to the
// shiptrace-cc-hook binary (callers usually resolve via exec.LookPath).
// Returns the number of hook entries actually added.
func (s *Settings) MergeShiptraceHooks(shiptraceBin string) int {
	if s.Hooks == nil {
		s.Hooks = map[HookEventName][]HookGroup{}
	}
	added := 0
	for _, h := range ShiptraceHooks {
		command := fmt.Sprintf("%s %s", shiptraceBin, h.Subcommand)
		if alreadyInstalled(s.Hooks[h.Event], h.Matcher, command) {
			continue
		}
		s.Hooks[h.Event] = append(s.Hooks[h.Event], HookGroup{
			Matcher: h.Matcher,
			Hooks:   []Hook{{Type: "command", Command: command}},
		})
		added++
	}
	return added
}

// alreadyInstalled returns true if any HookGroup in groups already carries
// a shiptrace command (matched by prefix on the binary path, not equality —
// the user may have rebuilt to a different path).
func alreadyInstalled(groups []HookGroup, matcher, fullCommand string) bool {
	for _, g := range groups {
		if g.Matcher != matcher {
			continue
		}
		for _, h := range g.Hooks {
			if isShiptraceHookCommand(h.Command) {
				return true
			}
			if h.Command == fullCommand {
				return true
			}
		}
	}
	return false
}

// isShiptraceHookCommand recognizes any command whose basename starts with
// "shiptrace-cc-hook", so a reinstall after the binary moved still
// short-circuits.
func isShiptraceHookCommand(cmd string) bool {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	return strings.Contains(filepath.Base(fields[0]), "shiptrace-cc-hook")
}

// HasShiptraceHooks reports whether all five shiptrace hook entries are
// present. Used by doctor.
func (s *Settings) HasShiptraceHooks() (present, missing []HookEventName) {
	for _, h := range ShiptraceHooks {
		if alreadyInstalled(s.Hooks[h.Event], h.Matcher, "") {
			present = append(present, h.Event)
		} else {
			missing = append(missing, h.Event)
		}
	}
	return
}

// DefaultSettingsPath returns ~/.claude/settings.json. Tests override the
// HOME env var to point elsewhere.
func DefaultSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ccsettings: user home: %w", err)
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}
