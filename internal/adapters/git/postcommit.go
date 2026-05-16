// Package git is the ship adapter for `git commit` events. The hot path
// lives in cmd/shiptrace-git-postcommit; this package holds the reusable
// pieces (commit-metadata collection, ship-event construction) so the
// command stays a thin shell around it.
//
// Like the CC hook, this package is stdlib-only: a post-commit hook should
// not slow down `git commit`.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/session"
)

// Provider is the value stamped on Provider for ship events emitted by
// this adapter.
const Provider = "git"

// CommitMetadata captures the slice of `git log` and `git diff-tree`
// output we need to emit a useful ship event.
type CommitMetadata struct {
	SHA          string
	ShortSHA     string
	Author       string
	Subject      string
	FilesTouched []string
	Insertions   int
	Deletions    int
	FilesChanged int
}

// CollectCommitMetadata shells out to git from inside repoDir and returns
// the metadata for HEAD. The caller is expected to pass an absolute path
// or a directory git can resolve.
//
// Optional fields (author, subject, files-touched, shortstat) use
// runGitOptional, which swallows failures by default but logs them to
// stderr when SHIPTRACE_DEBUG=1 — useful when a corrupted repo silently
// produces empty ship events.
func CollectCommitMetadata(repoDir string) (*CommitMetadata, error) {
	sha, err := runGit(repoDir, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git: rev-parse HEAD: %w", err)
	}
	short, err := runGit(repoDir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git: rev-parse --short: %w", err)
	}
	author := runGitOptional(repoDir, "log", "-1", "--format=%an")
	subject := runGitOptional(repoDir, "log", "-1", "--format=%s")
	// `git show --name-only` handles both the initial commit (no parent)
	// and ordinary commits, unlike `git diff-tree HEAD` which produces
	// nothing on the initial commit.
	filesRaw := runGitOptional(repoDir, "show", "--name-only", "--format=", "HEAD")
	statsRaw := runGitOptional(repoDir, "show", "--shortstat", "--format=", "HEAD")

	m := &CommitMetadata{
		SHA:      sha,
		ShortSHA: short,
		Author:   author,
		Subject:  subject,
	}
	for _, l := range strings.Split(filesRaw, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			m.FilesTouched = append(m.FilesTouched, l)
		}
	}
	parseShortstat(strings.TrimSpace(statsRaw), m)
	return m, nil
}

// parseShortstat handles lines like:
//
//	3 files changed, 12 insertions(+), 4 deletions(-)
//	1 file changed, 7 insertions(+)
//
// Best-effort: any field we can't parse falls back to zero, never errors.
func parseShortstat(line string, m *CommitMetadata) {
	if line == "" {
		return
	}
	for _, part := range strings.Split(line, ",") {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(fields[1], "file"):
			m.FilesChanged = n
		case strings.HasPrefix(fields[1], "insertion"):
			m.Insertions = n
		case strings.HasPrefix(fields[1], "deletion"):
			m.Deletions = n
		}
	}
}

// FindRepoRoot resolves the git toplevel for cwd. Returns an error when
// cwd isn't tracked by git — the post-commit hook can't run outside a repo.
func FindRepoRoot(cwd string) (string, error) {
	out, err := runGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git: not in a repo (%w)", err)
	}
	if out == "" {
		return "", errors.New("git: empty toplevel")
	}
	return out, nil
}

// runGit shells out to `git -C dir <args...>` and returns trimmed stdout.
func runGit(dir string, args ...string) (string, error) {
	all := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", all...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// runGitOptional wraps runGit for callers who never want a failure to abort
// the hook. On success it returns the trimmed stdout; on failure it returns
// "" and, when SHIPTRACE_DEBUG=1, writes a one-line diagnostic to stderr so
// a user investigating an empty ship event can see what git complained
// about. The post-commit hook must not print to stdout under any
// circumstances — that breaks shell composability with other hooks.
func runGitOptional(dir string, args ...string) string {
	out, err := runGit(dir, args...)
	if err != nil && os.Getenv("SHIPTRACE_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "shiptrace git: %v (args=%v)\n", err, args)
	}
	return out
}

// BuildShipEvent assembles a normalized ship event from commit metadata.
// sessionID may be empty for an unattributed commit (no active session).
// attributionMethod is "explicit" when a session was found, otherwise
// empty.
func BuildShipEvent(m *CommitMetadata, sessionID, attributionMethod string, now time.Time) events.Event {
	meta := map[string]any{
		"kind":   "commit",
		"sha":    m.SHA,
		"short":  m.ShortSHA,
		"author": m.Author,
	}
	if m.Subject != "" {
		meta["subject"] = m.Subject
	}
	if m.Insertions > 0 || m.Deletions > 0 || m.FilesChanged > 0 {
		meta["magnitude"] = map[string]any{
			"insertions":    m.Insertions,
			"deletions":     m.Deletions,
			"files_changed": m.FilesChanged,
		}
	}
	if attributionMethod != "" {
		meta["attribution_method"] = attributionMethod
	}
	meta["ref"] = m.SHA

	return events.Event{
		EventType:    events.Ship,
		Ts:           now,
		SessionID:    sessionID,
		Provider:     Provider,
		FilesTouched: m.FilesTouched,
		Metadata:     meta,
	}
}

// ResolveSession looks up the per-project pointer for repoRoot and returns
// (sessionID, label, method) where method is "explicit" when found and ""
// when the pointer is missing or stale (use the empty session id).
func ResolveSession(home, repoRoot string, now time.Time, maxAge time.Duration) (id, label, method string, err error) {
	path, err := session.ProjectPointerPath(home, repoRoot)
	if err != nil {
		return "", "", "", err
	}
	ptr, err := session.ReadActive(path)
	if err != nil {
		return "", "", "", err
	}
	if ptr == nil {
		return "", "", "", nil
	}
	if ptr.IsStale(now, maxAge) {
		return "", "", "", nil
	}
	return ptr.SessionID, ptr.Label, "explicit", nil
}
