// Package display owns the CLI's user-facing output: attribution feedback
// lines, color toggling, and human-friendly time formatting. The point of
// every line printed here is that silent miscategorization should be
// impossible.
package display

import (
	"io"
	"os"
)

// ANSI escape codes — kept inline so we don't pull a color library.
const (
	reset  = "\033[0m"
	green  = "\033[32m"
	yellow = "\033[33m"
	red    = "\033[31m"
	dim    = "\033[2m"
)

// Color controls whether ANSI escapes are emitted. Default behavior:
//   - Honor NO_COLOR (https://no-color.org) — any non-empty value disables.
//   - Disable when output is not a TTY (e.g. piped to a file).
//
// Tests inject this via NewColor to keep output deterministic.
type Color struct {
	enabled bool
}

// DefaultColor returns the Color appropriate for stdout. Pass the same
// io.Writer you intend to write to — the TTY check uses it.
func DefaultColor(w io.Writer) Color {
	if os.Getenv("NO_COLOR") != "" {
		return Color{enabled: false}
	}
	return Color{enabled: isTerminal(w)}
}

// NewColor builds a Color with explicit enablement, for tests.
func NewColor(enabled bool) Color {
	return Color{enabled: enabled}
}

func (c Color) wrap(code, s string) string {
	if !c.enabled {
		return s
	}
	return code + s + reset
}

// Green wraps s in a green ANSI sequence when colors are enabled.
func (c Color) Green(s string) string { return c.wrap(green, s) }

// Yellow wraps s in a yellow ANSI sequence when colors are enabled.
func (c Color) Yellow(s string) string { return c.wrap(yellow, s) }

// Red wraps s in a red ANSI sequence when colors are enabled.
func (c Color) Red(s string) string { return c.wrap(red, s) }

// Dim wraps s in a dim ANSI sequence when colors are enabled.
func (c Color) Dim(s string) string { return c.wrap(dim, s) }
