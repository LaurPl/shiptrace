package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// `shiptrace doctor` lands on day 2 once we have hooks to verify. For now
// it's hidden but registered so the command tree is stable.
func newDoctorCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:    "doctor",
		Short:  "Verify hooks, watchers, adapters (day 2)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(out, "shiptrace doctor: not implemented yet (lands day 2 with the Claude Code hook)")
			return nil
		},
	}
}
