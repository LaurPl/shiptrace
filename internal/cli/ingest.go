package cli

import (
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/ingest"
	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/store"
)

func newIngestCommand(stdout, stderr io.Writer) *cobra.Command {
	var once bool
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Materialize JSONL events into SQLite (foreground daemon)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := paths.DBPath()
			if err != nil {
				return err
			}
			s, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			eventsDir, err := paths.EventsDir()
			if err != nil {
				return err
			}
			checkpointPath, err := paths.CheckpointPath()
			if err != nil {
				return err
			}

			ing := ingest.New(s, eventsDir, checkpointPath)
			ing.SetLogger(func(format string, args ...any) {
				fmt.Fprintf(stderr, format+"\n", args...)
			})

			if once {
				return ing.IngestOnce(cmd.Context())
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			fmt.Fprintln(stdout, "shiptrace: ingesting, watching", eventsDir)
			fmt.Fprintln(stdout, "shiptrace: press Ctrl-C to stop")
			return ing.Run(ctx)
		},
	}
	cmd.Flags().BoolVar(&once, "once", false, "Run a single ingest pass and exit (no fsnotify watch)")
	return cmd
}
