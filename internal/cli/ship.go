package cli

import (
	"errors"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/attrib"
	"github.com/LaurPl/shiptrace/internal/display"
	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/paths"
)

func newShipCommand(out io.Writer) *cobra.Command {
	var (
		sessionFlag string
		kind        string
	)
	cmd := &cobra.Command{
		Use:   "ship <description>",
		Short: "Log a ship event (anything that counts as shipped in your domain)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			description := args[0]
			if description == "" {
				return errors.New("description cannot be empty")
			}
			pointerPath, err := paths.PointerPath()
			if err != nil {
				return err
			}
			r, err := attrib.Resolve(sessionFlag, pointerPath)
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

			now := time.Now().UTC()
			meta := map[string]any{
				"kind":        kind,
				"description": description,
			}
			if r.Source != attrib.SourceNone {
				meta["attribution_method"] = "explicit"
			}
			if err := w.Append(events.Event{
				EventType: events.Ship,
				Ts:        now,
				SessionID: r.SessionID,
				Metadata:  meta,
			}); err != nil {
				return err
			}

			c := display.DefaultColor(out)
			display.ShipResult(out, c, r, now)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "Attribute this ship to a specific session ID")
	cmd.Flags().StringVar(&kind, "kind", "manual", "Ship kind (manual | commit | publish | …)")
	return cmd
}
