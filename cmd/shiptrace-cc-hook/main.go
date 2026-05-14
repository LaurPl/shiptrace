// Command shiptrace-cc-hook receives Claude Code hook JSON on stdin and
// appends a normalized event to the shiptrace eventlog.
//
// This binary is on the hot path of every UserPromptSubmit / PostToolUse
// and MUST stay under 30ms p99. To that end:
//   - stdlib-only dependencies (no cobra, no fsnotify, no sqlite)
//   - subcommand dispatched by a bare switch on os.Args[1]
//   - eventlog opened lazily, fsynced per append (durability over latency
//     because the per-write fsync is microseconds on modern SSDs)
//
// Usage:
//
//	shiptrace-cc-hook <subcommand>
//
// Subcommands match the CC hook event names, lowercased and dash-joined:
//
//	session-start | prompt | tool-use | subagent-stop | stop
//
// Hook payload JSON is read from stdin. Exit code is 0 on success, 1 on
// error. Errors are written to stderr but never printed to stdout — CC
// hooks treat any stdout output as feedback to the model, and we don't
// want shiptrace babbling into the conversation.
package main

import (
	"fmt"
	"os"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/hooks/claudecode"
	"github.com/LaurPl/shiptrace/internal/paths"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "shiptrace-cc-hook: subcommand required (session-start | prompt | tool-use | subagent-stop | stop)")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "shiptrace-cc-hook:", err)
		os.Exit(1)
	}
}

func run(subcommand string) error {
	payload, err := claudecode.ParsePayload(os.Stdin)
	if err != nil {
		return fmt.Errorf("parse stdin: %w", err)
	}

	home, err := paths.Home()
	if err != nil {
		return err
	}
	eventsDir, err := paths.EventsDir()
	if err != nil {
		return err
	}
	w, err := eventlog.New(eventsDir)
	if err != nil {
		return err
	}
	defer w.Close()
	sessions, err := claudecode.NewSessionMap(home)
	if err != nil {
		return err
	}
	h := claudecode.New(w, sessions, home)

	switch subcommand {
	case "session-start":
		return h.HandleSessionStart(payload)
	case "prompt":
		return h.HandlePrompt(payload)
	case "tool-use":
		return h.HandleToolUse(payload)
	case "subagent-stop":
		return h.HandleSubagentStop(payload)
	case "stop":
		return h.HandleStop(payload)
	default:
		return fmt.Errorf("unknown subcommand %q", subcommand)
	}
}
