package display

import (
	"fmt"
	"io"
	"time"

	"github.com/LaurPl/shiptrace/internal/attrib"
)

// SessionStarted prints the success line after `shiptrace session start`.
func SessionStarted(w io.Writer, c Color, sessionID, label string) {
	fmt.Fprintf(w, "%s Session started: %q (%s)\n",
		c.Green("✓"),
		label,
		c.Dim(sessionID),
	)
}

// SessionStopped prints the success line after `shiptrace session stop`.
func SessionStopped(w io.Writer, c Color, sessionID, label string, started, ended time.Time) {
	dur := Duration(ended.Sub(started))
	fmt.Fprintf(w, "%s Session stopped: %q (%s, duration %s)\n",
		c.Green("✓"),
		label,
		c.Dim(sessionID),
		dur,
	)
}

// ShipResult prints the feedback line after `shiptrace ship`. The whole point
// of this package: every ship event shows which session it was attributed to
// — silent miscategorization is the only footgun we cannot live with.
func ShipResult(w io.Writer, c Color, r *attrib.Resolution, now time.Time) {
	if r.Conflict != nil {
		fmt.Fprintf(w, "%s Session conflict: %s=%s overrides %s=%s (using %s)\n",
			c.Yellow("⚠"),
			r.Source, r.SessionID,
			r.Conflict.LosingSource, r.Conflict.LosingSessionID,
			r.SessionID,
		)
	}
	switch r.Source {
	case attrib.SourceNone:
		fmt.Fprintf(w, "%s Ship event logged unattributed (no active session). Use --session=ID or `shiptrace session start` first.\n",
			c.Yellow("⚠"),
		)
	case attrib.SourcePointer:
		// We have a label and StartedAt — print the full friendly line.
		fmt.Fprintf(w, "%s Ship event logged → session %q (%s, started %s)\n",
			c.Green("✓"),
			r.Label,
			c.Dim(r.SessionID),
			Relative(r.StartedAt, now),
		)
	default:
		// Flag or env: we only have the ID. Promise to enrich in v0.2 when
		// the resolver gains read-from-store. For now the ID alone is honest.
		fmt.Fprintf(w, "%s Ship event logged → session %s (via %s)\n",
			c.Green("✓"),
			c.Dim(r.SessionID),
			r.Source,
		)
	}
}
