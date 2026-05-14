// Package attrib resolves which session a command should attribute its event
// to. Precedence (top wins):
//  1. explicit --session flag
//  2. $SHIPTRACE_SESSION_ID env var
//  3. per-project pointer at ~/.shiptrace/project-pointers/<hash>.json
//  4. ~/.shiptrace/.current-session pointer file (manual recorder)
//  5. none -> unattributed
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
	SourceFlag           Source = "flag"
	SourceEnv            Source = "env"
	SourceProjectPointer Source = "project-pointer"
	SourcePointer        Source = "pointer"
	SourceNone           Source = "none"
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

// Inputs bundles the precedence-chain sources so callers can fill in
// whichever they have available. Empty paths and zero values are skipped.
type Inputs struct {
	FlagValue          string // --session flag
	EnvValue           string // typically os.Getenv(EnvVar); read by Resolve when empty
	ProjectPointerPath string // per-project pointer (CC's project pointer)
	GlobalPointerPath  string // ~/.shiptrace/.current-session (manual recorder)
	Now                time.Time
	MaxStaleness       time.Duration // for pointer freshness; zero means accept any age
}

// Resolve consults the inputs in precedence order. Stale project pointers
// (per Inputs.MaxStaleness) are skipped so a forgotten CC session doesn't
// attribute tomorrow's `shiptrace ship`.
func Resolve(in Inputs) (*Resolution, error) {
	flag := in.FlagValue
	env := in.EnvValue
	if env == "" {
		env = os.Getenv(EnvVar)
	}

	projectPtr, err := readPointerIfFresh(in.ProjectPointerPath, in.Now, in.MaxStaleness)
	if err != nil {
		return nil, err
	}
	globalPtr, err := readPointerIfFresh(in.GlobalPointerPath, in.Now, in.MaxStaleness)
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
	case projectPtr != nil:
		return &Resolution{
			SessionID: projectPtr.SessionID,
			Label:     projectPtr.Label,
			StartedAt: projectPtr.StartedAt,
			Source:    SourceProjectPointer,
		}, nil
	case globalPtr != nil:
		return &Resolution{
			SessionID: globalPtr.SessionID,
			Label:     globalPtr.Label,
			StartedAt: globalPtr.StartedAt,
			Source:    SourcePointer,
		}, nil
	default:
		return &Resolution{Source: SourceNone}, nil
	}
}

func readPointerIfFresh(path string, now time.Time, maxAge time.Duration) (*session.ActivePointer, error) {
	if path == "" {
		return nil, nil
	}
	p, err := session.ReadActive(path)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	if maxAge > 0 && p.IsStale(now, maxAge) {
		return nil, nil
	}
	return p, nil
}
