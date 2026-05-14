package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/ccsettings"
)

// HookBinaryName is the binary the init command looks for on PATH (and
// falls back to). Kept as a constant so doctor can use the same value.
const HookBinaryName = "shiptrace-cc-hook"

func newInitCommand(out io.Writer) *cobra.Command {
	var (
		yes          bool
		hookBinFlag  string
		settingsFlag string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Install shiptrace hooks into ~/.claude/settings.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.InOrStdin(), out, yes, hookBinFlag, settingsFlag)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the confirmation prompt")
	cmd.Flags().StringVar(&hookBinFlag, "hook-bin", "", "Override path to shiptrace-cc-hook (default: resolve via $PATH)")
	cmd.Flags().StringVar(&settingsFlag, "settings", "", "Override path to settings.json (default: ~/.claude/settings.json)")
	return cmd
}

func runInit(in io.Reader, out io.Writer, yes bool, hookBinFlag, settingsFlag string) error {
	hookBin, err := resolveHookBinary(hookBinFlag)
	if err != nil {
		return err
	}
	settingsPath := settingsFlag
	if settingsPath == "" {
		settingsPath, err = ccsettings.DefaultSettingsPath()
		if err != nil {
			return err
		}
	}

	settings, err := ccsettings.Read(settingsPath)
	if err != nil {
		return err
	}
	pretty := func(s *ccsettings.Settings) string { return prettyHooks(s.Hooks) }
	before := pretty(settings)

	added := settings.MergeShiptraceHooks(hookBin)
	if added == 0 {
		fmt.Fprintln(out, "shiptrace hooks already installed — nothing to do.")
		return nil
	}

	fmt.Fprintln(out, "Will write to:", settingsPath)
	fmt.Fprintln(out, "Using hook binary:", hookBin)
	fmt.Fprintln(out, "Adding", added, "hook entries.")
	fmt.Fprintln(out, "---")
	fmt.Fprintln(out, "BEFORE (hooks section):")
	fmt.Fprintln(out, before)
	fmt.Fprintln(out, "AFTER (hooks section):")
	fmt.Fprintln(out, pretty(settings))
	fmt.Fprintln(out, "---")

	if !yes {
		ok, err := confirm(in, out, "Apply this change?")
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(out, "Aborted; settings.json unchanged.")
			return nil
		}
	}

	if err := ccsettings.Write(settingsPath, settings); err != nil {
		return err
	}
	fmt.Fprintln(out, "Wrote", settingsPath)
	fmt.Fprintln(out, "Run `shiptrace doctor` to verify the hooks fire.")
	return nil
}

// resolveHookBinary prefers an explicit --hook-bin path, then $PATH, then
// the same directory as the running shiptrace binary (helpful for the
// common "I built but haven't installed" case).
func resolveHookBinary(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("shiptrace init: --hook-bin not found: %w", err)
		}
		return abs, nil
	}
	if path, err := exec.LookPath(HookBinaryName); err == nil {
		return path, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), HookBinaryName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("shiptrace init: could not find shiptrace-cc-hook on PATH or next to shiptrace. Pass --hook-bin <path> or `go install ./cmd/shiptrace-cc-hook` first.")
}

func confirm(in io.Reader, out io.Writer, question string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N] ", question)
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}

// prettyHooks renders the hooks map deterministically for the diff display.
// It's not real JSON formatting — we just want a stable, readable summary
// that's grep-friendly.
func prettyHooks(hooks map[ccsettings.HookEventName][]ccsettings.HookGroup) string {
	if len(hooks) == 0 {
		return "  (none)"
	}
	keys := []ccsettings.HookEventName{
		ccsettings.SessionStart,
		ccsettings.UserPromptSubmit,
		ccsettings.PostToolUse,
		ccsettings.SubagentStop,
		ccsettings.Stop,
	}
	var sb strings.Builder
	for _, k := range keys {
		groups, ok := hooks[k]
		if !ok || len(groups) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "  %s:\n", k)
		for _, g := range groups {
			if g.Matcher != "" {
				fmt.Fprintf(&sb, "    - matcher: %q\n", g.Matcher)
			}
			for _, h := range g.Hooks {
				fmt.Fprintf(&sb, "      • %s\n", h.Command)
			}
		}
	}
	if sb.Len() == 0 {
		return "  (none)"
	}
	return strings.TrimRight(sb.String(), "\n")
}
