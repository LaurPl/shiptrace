package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/store"
)

func newReportCommand(out io.Writer) *cobra.Command {
	var (
		week  bool
		month bool
		day   bool
		days  int
	)
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Plaintext summary of recent shiptrace activity",
		RunE: func(cmd *cobra.Command, args []string) error {
			window := pickWindowDays(day, week, month, days)
			dbPath, err := paths.DBPath()
			if err != nil {
				return err
			}
			s, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			return printReport(cmd.Context(), out, s, window)
		},
	}
	cmd.Flags().BoolVar(&day, "day", false, "Cover the last 24 hours")
	cmd.Flags().BoolVar(&week, "week", false, "Cover the last 7 days")
	cmd.Flags().BoolVar(&month, "month", false, "Cover the last 30 days")
	cmd.Flags().IntVar(&days, "days", 0, "Custom window in days (overrides --day/--week/--month)")
	return cmd
}

func pickWindowDays(day, week, month bool, custom int) int {
	if custom > 0 {
		return custom
	}
	switch {
	case day:
		return 1
	case week:
		return 7
	case month:
		return 30
	default:
		return 7
	}
}

func printReport(ctx context.Context, out io.Writer, s *store.Store, days int) error {
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Unix()

	fmt.Fprintf(out, "shiptrace report — last %d day(s)\n", days)
	fmt.Fprintln(out, strings.Repeat("─", 60))

	// Headline numbers.
	var totalSessions, totalShips int
	row := s.DB().QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM sessions WHERE start_ts >= ?),
			(SELECT COUNT(*) FROM ship_events WHERE ts >= ?)
	`, cutoff, cutoff)
	if err := row.Scan(&totalSessions, &totalShips); err != nil {
		return err
	}
	ratio := "—"
	if totalShips > 0 {
		ratio = fmt.Sprintf("%.2f", float64(totalSessions)/float64(totalShips))
	}
	fmt.Fprintf(out, "  sessions:           %d\n", totalSessions)
	fmt.Fprintf(out, "  ships:              %d\n", totalShips)
	fmt.Fprintf(out, "  sessions-to-ship:   %s\n", ratio)
	fmt.Fprintln(out)

	// Per-project breakdown.
	rows, err := s.DB().QueryContext(ctx, `
		SELECT
			COALESCE(s.project, '(unassigned)') AS project,
			COUNT(DISTINCT s.id) AS sessions,
			COALESCE(SUM((SELECT COUNT(*) FROM ship_events WHERE session_id = s.id)), 0) AS ships,
			AVG(s.replan_score) AS mean_replan
		FROM sessions s
		WHERE s.start_ts >= ?
		GROUP BY project
		ORDER BY sessions DESC
	`, cutoff)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Fprintf(out, "  %-22s %8s %6s %12s %10s\n", "project", "sessions", "ships", "sess/ship", "avg replan")
	fmt.Fprintln(out, "  "+strings.Repeat("─", 58))
	for rows.Next() {
		var (
			project    string
			sessions   int
			ships      int
			meanReplan float64
		)
		if err := rows.Scan(&project, &sessions, &ships, &meanReplan); err != nil {
			return err
		}
		ratioStr := "—"
		if ships > 0 {
			ratioStr = fmt.Sprintf("%.2f", float64(sessions)/float64(ships))
		}
		fmt.Fprintf(out, "  %-22s %8d %6d %12s %10.2f\n",
			truncate(project, 22), sessions, ships, ratioStr, meanReplan,
		)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if totalSessions == 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  (no data in this window — run `shiptrace session start` or attach the CC hook)")
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
