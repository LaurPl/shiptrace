// Package attrib resolves which session a command should attribute its event
// to. Precedence (top wins):
//  1. explicit --session flag
//  2. $SHIPTRACE_SESSION_ID env var
//  3. ~/.shiptrace/.current-session pointer file
//  4. none -> unattributed
//
// Conflicts between sources 1 and 2 are surfaced so the CLI can print the
// "session conflict" warning. The point of the whole package is that
// miscategorization should never be silent.
package attrib

import (
	"os"
	"time"

	"github.com/LaurPl/shiptrace/internal/session"
)

// EnvVar is the env var consulted as source 2 in the precedence chain.
const EnvVar = "SHIPTRACE_SESSION_ID"

// Source identifies which precedence slot won.
type Source string

const (
	SourceFlag    Source = "flag"
	SourceEnv     Source = "env"
	SourcePointer Source = "pointer"
	SourceNone    Source = "none"
)

// Conflict records a lower-precedence source that disagreed with the winner.
// Surfaced to the user so silent overrides become visible.
type Conflict struct {
	LosingSource    Source
	LosingSessionID string
}

// Resolution is the outcome of Resolve.
type Resolution struct {
	SessionID string
	Label     string    // populated when Source is Pointer
	StartedAt time.Time // populated when Source is Pointer

	Source   Source
	Conflict *Conflict
}

// Resolve consults flag/env/pointer in precedence order. flagValue is whatever
// the cobra command parsed from --session (empty string if unset). pointerPath
// is the resolved path to the active-session marker (callers pass
// paths.PointerPath()).
func Resolve(flagValue, pointerPath string) (*Resolution, error) {
	flag := flagValue
	env := os.Getenv(EnvVar)
	ptr, err := session.ReadActive(pointerPath)
	if err != nil {
		return nil, err
	}

	switch {
	case flag != "":
		r := &Resolution{SessionID: flag, Source: SourceFlag}
		if env != "" && env != flag {
			r.Conflict = &Conflict{LosingSource: SourceEnv, LosingSessionID: env}
		}
		return r, nil
	case env != "":
		return &Resolution{SessionID: env, Source: SourceEnv}, nil
	case ptr != nil:
		return &Resolution{
			SessionID: ptr.SessionID,
			Label:     ptr.Label,
			StartedAt: ptr.StartedAt,
			Source:    SourcePointer,
		}, nil
	default:
		return &Resolution{Source: SourceNone}, nil
	}
}
