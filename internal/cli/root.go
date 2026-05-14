// Package cli is the thin orchestration layer for the shiptrace binary.
// Each subcommand opens whatever it needs (eventlog writer, store, pointer
// file), wires the pieces together, and prints the attribution feedback line.
// Business logic lives in the internal/* packages — this package should
// never grow beyond glue.
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Execute runs the root command. main.go calls this and translates a non-nil
// error into a non-zero exit code.
func Execute() error {
	root := NewRootCommand(os.Stdout, os.Stderr)
	return root.Execute()
}

// NewRootCommand assembles the command tree. Exposed so tests can drive it
// without going through os.Args.
func NewRootCommand(stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "shiptrace",
		Short:         "Session-to-ship telemetry for AI agents",
		Long:          "shiptrace records what your AI agents did and joins those events to what actually shipped.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(
		newSessionCommand(stdout),
		newShipCommand(stdout),
		newIngestCommand(stdout, stderr),
		newInitCommand(stdout),
		newDoctorCommand(stdout),
		newAdapterCommand(stdout, stderr),
		newServeCommand(stdout, stderr),
		newReportCommand(stdout),
	)
	return root
}

// printErr writes a uniform "error: ..." line to stderr. cobra also prints the
// raw error; this is for cases where we want to short-circuit before cobra
// gets involved.
func printErr(w io.Writer, err error) {
	fmt.Fprintln(w, "error:", err)
}
