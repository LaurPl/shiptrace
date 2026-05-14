package cli

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/attrib"
	"github.com/LaurPl/shiptrace/internal/display"
	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/session"
)

func newSessionCommand(out io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage manual sessions",
	}
	cmd.AddCommand(newSessionStartCommand(out), newSessionStopCommand(out))
	return cmd
}

func newSessionStartCommand(out io.Writer) *cobra.Command {
	var (
		project  string
		provider string
	)
	cmd := &cobra.Command{
		Use:   "start <label>",
		Short: "Start a new manual session with a human-readable label",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			label := args[0]
			if label == "" {
				return errors.New("label cannot be empty")
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

			id := events.NewSessionID()
			now := time.Now().UTC()
			ev := events.Event{
				EventType: events.SessionStart,
				Ts:        now,
				SessionID: id,
				Provider:  provider,
				Project:   project,
				Label:     label,
			}
			if err := w.Append(ev); err != nil {
				return err
			}

			pointerPath, err := paths.PointerPath()
			if err != nil {
				return err
			}
			if err := session.WriteActive(pointerPath, session.ActivePointer{
				SessionID: id,
				Label:     label,
				StartedAt: now,
			}); err != nil {
				return err
			}

			c := display.DefaultColor(out)
			display.SessionStarted(out, c, id, label)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Project name to attribute this session to")
	cmd.Flags().StringVar(&provider, "provider", "manual", "Provider label (default: manual)")
	return cmd
}

func newSessionStopCommand(out io.Writer) *cobra.Command {
	var sessionFlag string
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the active manual session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			pointerPath, err := paths.PointerPath()
			if err != nil {
				return err
			}
			r, err := attrib.Resolve(attrib.Inputs{
				FlagValue:         sessionFlag,
				GlobalPointerPath: pointerPath,
			})
			if err != nil {
				return err
			}
			if r.Source == attrib.SourceNone {
				return fmt.Errorf("no active session to stop (start one with `shiptrace session start <label>`)")
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
			if err := w.Append(events.Event{
				EventType: events.SessionStop,
				Ts:        now,
				SessionID: r.SessionID,
			}); err != nil {
				return err
			}

			// Only clear the pointer if that's actually the source we used —
			// a --session=X stop should not yank someone else's pointer.
			if r.Source == attrib.SourcePointer {
				if err := session.ClearActive(pointerPath); err != nil {
					return err
				}
			}

			c := display.DefaultColor(out)
			label := r.Label
			if label == "" {
				label = "(unlabeled)"
			}
			started := r.StartedAt
			if started.IsZero() {
				started = now
			}
			display.SessionStopped(out, c, r.SessionID, label, started, now)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionFlag, "session", "", "Explicit session ID to stop (overrides pointer/env)")
	return cmd
}
