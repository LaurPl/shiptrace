package ccsettings

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMissingFileReturnsEmpty(t *testing.T) {
	s, err := Read(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s == nil || len(s.Hooks) != 0 {
		t.Fatalf("expected empty settings, got %+v", s)
	}
}

func TestReadPreservesUnknownTopLevelKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := `{"theme":"dark","permissions":{"allow":["foo"]}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, ok := s.Extras["theme"]; !ok {
		t.Errorf("theme missing")
	}
	if _, ok := s.Extras["permissions"]; !ok {
		t.Errorf("permissions missing")
	}
}

func TestWritePreservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	raw := `{"theme":"dark"}`
	_ = os.WriteFile(path, []byte(raw), 0o600)
	s, _ := Read(path)
	s.MergeShiptraceHooks("/usr/local/bin/shiptrace-cc-hook")
	if err := Write(path, s); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	var back map[string]json.RawMessage
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := back["theme"]; !ok {
		t.Errorf("theme dropped on rewrite")
	}
	if _, ok := back["hooks"]; !ok {
		t.Errorf("hooks missing from rewrite")
	}
}

func TestMergeAddsAllHooks(t *testing.T) {
	s := &Settings{}
	added := s.MergeShiptraceHooks("/usr/local/bin/shiptrace-cc-hook")
	if added != len(ShiptraceHooks) {
		t.Fatalf("added: %d want %d", added, len(ShiptraceHooks))
	}
	for _, h := range ShiptraceHooks {
		groups := s.Hooks[h.Event]
		if len(groups) != 1 {
			t.Errorf("event %s: %d groups", h.Event, len(groups))
		}
		if !strings.Contains(groups[0].Hooks[0].Command, h.Subcommand) {
			t.Errorf("event %s command: %q", h.Event, groups[0].Hooks[0].Command)
		}
	}
}

func TestMergeIdempotent(t *testing.T) {
	s := &Settings{}
	first := s.MergeShiptraceHooks("/path/shiptrace-cc-hook")
	second := s.MergeShiptraceHooks("/path/shiptrace-cc-hook")
	if first != len(ShiptraceHooks) || second != 0 {
		t.Errorf("first=%d second=%d want %d/0", first, second, len(ShiptraceHooks))
	}
}

func TestMergeIdempotentAcrossBinaryPaths(t *testing.T) {
	s := &Settings{}
	s.MergeShiptraceHooks("/old/path/shiptrace-cc-hook")
	added := s.MergeShiptraceHooks("/new/path/shiptrace-cc-hook")
	if added != 0 {
		t.Errorf("expected 0 added after path change, got %d", added)
	}
}

func TestMergePreservesUnrelatedHooks(t *testing.T) {
	s := &Settings{
		Hooks: map[HookEventName][]HookGroup{
			Stop: {{Hooks: []Hook{{Type: "command", Command: "/usr/local/bin/some-other-tool"}}}},
		},
	}
	s.MergeShiptraceHooks("/path/shiptrace-cc-hook")
	stops := s.Hooks[Stop]
	if len(stops) != 2 {
		t.Fatalf("expected 2 Stop entries (other + shiptrace), got %d", len(stops))
	}
	foundOther := false
	for _, g := range stops {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, "some-other-tool") {
				foundOther = true
			}
		}
	}
	if !foundOther {
		t.Errorf("user's existing Stop hook was dropped")
	}
}

// TestMergeQuotesShellHostileBinaryPaths confirms that paths with shell
// metacharacters end up as a single literal argv[0] when /bin/sh parses
// the resulting command string — defending against the case where CC
// executes the hook command through a shell.
func TestMergeQuotesShellHostileBinaryPaths(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	for _, p := range []string{
		"/usr/local/bin/shiptrace-cc-hook",
		"/tmp/path with space/shiptrace-cc-hook",
		"/tmp/$(echo evil)/shiptrace-cc-hook",
		"/tmp/`whoami`/shiptrace-cc-hook",
	} {
		t.Run(p, func(t *testing.T) {
			s := &Settings{}
			s.MergeShiptraceHooks(p)
			cmd := s.Hooks[SessionStart][0].Hooks[0].Command
			// Use `sh -c 'set -- <cmd>; printf %s\n "$1"'` to extract the
			// first shell token; assert it equals p verbatim.
			script := "set -- " + cmd + `; printf %s "$1"`
			out, err := exec.Command("sh", "-c", script).Output()
			if err != nil {
				t.Fatalf("sh failed for %s: %v", cmd, err)
			}
			if string(out) != p {
				t.Errorf("first token mismatch:\n  expected: %q\n  got:      %q\n  command:  %s", p, string(out), cmd)
			}
		})
	}
}

// TestMergeIdempotentAcrossQuotingStyles confirms that an existing install
// using the legacy unquoted command string is recognized on reinstall, so
// users who installed before the shellquote migration don't end up with
// duplicate entries.
func TestMergeIdempotentAcrossQuotingStyles(t *testing.T) {
	s := &Settings{
		Hooks: map[HookEventName][]HookGroup{
			SessionStart: {{Hooks: []Hook{{
				Type:    "command",
				Command: "/usr/local/bin/shiptrace-cc-hook session-start", // legacy unquoted form
			}}}},
		},
	}
	added := s.MergeShiptraceHooks("/usr/local/bin/shiptrace-cc-hook")
	// All but SessionStart added; SessionStart already present as a legacy
	// unquoted entry and must be recognized rather than duplicated.
	if want := len(ShiptraceHooks) - 1; added != want {
		t.Errorf("expected %d added (SessionStart recognized as legacy), got %d", want, added)
	}
}

func TestHasShiptraceHooks(t *testing.T) {
	s := &Settings{}
	s.MergeShiptraceHooks("/path/shiptrace-cc-hook")
	present, missing := s.HasShiptraceHooks()
	if len(missing) != 0 || len(present) != len(ShiptraceHooks) {
		t.Errorf("present=%v missing=%v", present, missing)
	}

	empty := &Settings{}
	_, missing2 := empty.HasShiptraceHooks()
	if len(missing2) != len(ShiptraceHooks) {
		t.Errorf("empty should miss %d, got %d", len(ShiptraceHooks), len(missing2))
	}
}
