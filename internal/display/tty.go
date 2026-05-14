package display

import (
	"io"
	"os"

	"golang.org/x/term"
)

// isTerminal returns true when w writes to an interactive terminal.
// We probe by extracting an *os.File from the writer (the common case
// being os.Stdout); anything else is assumed non-TTY, which is the
// correct default for tests, pipes, and file redirection.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
