// Command shiptrace-git-postcommit runs out of .git/hooks/post-commit and
// emits a ship event for the just-created commit. It deliberately does
// the bare minimum on the hot path:
//
//   - resolve the repo root via `git rev-parse --show-toplevel`
//   - read the per-project session pointer (if any, and if not stale)
//   - collect commit metadata (SHA, files, author, shortstat)
//   - append a ship event to the eventlog and exit
//
// All output is suppressed by default — a post-commit hook that babbles
// breaks shell composability. Errors go to stderr only.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/LaurPl/shiptrace/internal/adapters/git"
	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/session"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shiptrace-git-postcommit:", err)
		os.Exit(1)
	}
}

func run() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	repoRoot, err := git.FindRepoRoot(cwd)
	if err != nil {
		return err
	}
	meta, err := git.CollectCommitMetadata(repoRoot)
	if err != nil {
		return err
	}

	home, err := paths.Home()
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	sessionID, _, method, err := git.ResolveSession(home, repoRoot, now, session.DefaultMaxStaleness)
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

	return w.Append(git.BuildShipEvent(meta, sessionID, method, now))
}
