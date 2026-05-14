package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// `shiptrace init` lands on day 2 alongside the Claude Code hook installer.
// The command exists today so help output is stable and so `shiptrace --help`
// already advertises the surface area.
func newInitCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:    "init",
		Short:  "Detect providers and scaffold config (day 2)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(out, "shiptrace init: not implemented yet (lands day 2 with the Claude Code hook)")
			return nil
		},
	}
}
