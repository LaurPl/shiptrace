// Package shellquote produces single-quoted POSIX-shell tokens that survive
// any expansion phase intact. Used by hook installers that write user-
// supplied paths into files executed by /bin/sh.
//
// The strategy is the standard one: wrap in single quotes, escape any
// embedded single quote as '\'' (close, escape, reopen). Within single
// quotes the shell performs no expansion at all — $, backtick, \, newline,
// and even glob characters are taken literally. That's exactly what we want
// for a path: we don't trust it, we don't intend to expand it.
package shellquote

import "strings"

// Quote returns s wrapped so any POSIX shell parses it as a single literal
// word. The empty string round-trips as ''.
func Quote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
