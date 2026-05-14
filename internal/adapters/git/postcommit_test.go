package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/session"
)

func TestParseShortstatVariants(t *testing.T) {
	cases := []struct {
		in                                       string
		wantFiles, wantInsertions, wantDeletions int
	}{
		{" 3 files changed, 12 insertions(+), 4 deletions(-)", 3, 12, 4},
		{" 1 file changed, 7 insertions(+)", 1, 7, 0},
		{" 1 file changed, 1 deletion(-)", 1, 0, 1},
		{"", 0, 0, 0},
		{"garbage", 0, 0, 0},
	}
	for _, c := range cases {
		var m CommitMetadata
		parseShortstat(c.in, &m)
		if m.FilesChanged != c.wantFiles || m.Insertions != c.wantInsertions || m.Deletions != c.wantDeletions {
			t.Errorf("parseShortstat(%q) = files=%d insertions=%d deletions=%d, want %d/%d/%d",
				c.in, m.FilesChanged, m.Insertions, m.Deletions, c.wantFiles, c.wantInsertions, c.wantDeletions)
		}
	}
}

func TestBuildShipEventCarriesCommitFields(t *testing.T) {
	now := time.Date(2026, 5, 14, 11, 0, 0, 0, time.UTC)
	m := &CommitMetadata{
		SHA: "abc123", ShortSHA: "abc1234", Author: "Lau",
		Subject:      "feat: do the thing",
		FilesTouched: []string{"a.go", "b.go"},
		Insertions:   12, Deletions: 4, FilesChanged: 2,
	}
	ev := BuildShipEvent(m, "shp_xyz", "explicit", now)
	if ev.EventType != events.Ship {
		t.Errorf("EventType: %q", ev.EventType)
	}
	if ev.Provider != Provider {
		t.Errorf("Provider: %q", ev.Provider)
	}
	if ev.SessionID != "shp_xyz" {
		t.Errorf("SessionID: %q", ev.SessionID)
	}
	if ev.Metadata["kind"] != "commit" {
		t.Errorf("kind: %v", ev.Metadata["kind"])
	}
	if ev.Metadata["sha"] != "abc123" || ev.Metadata["short"] != "abc1234" {
		t.Errorf("sha fields wrong: %+v", ev.Metadata)
	}
	mag, ok := ev.Metadata["magnitude"].(map[string]any)
	if !ok {
		t.Fatalf("magnitude missing: %+v", ev.Metadata)
	}
	if mag["insertions"] != 12 || mag["files_changed"] != 2 {
		t.Errorf("magnitude wrong: %+v", mag)
	}
	if ev.Metadata["attribution_method"] != "explicit" {
		t.Errorf("attribution_method: %v", ev.Metadata["attribution_method"])
	}
	if len(ev.FilesTouched) != 2 {
		t.Errorf("FilesTouched: %v", ev.FilesTouched)
	}
}

func TestBuildShipEventUnattributedOmitsAttributionMethod(t *testing.T) {
	ev := BuildShipEvent(&CommitMetadata{SHA: "x"}, "", "", time.Now())
	if _, ok := ev.Metadata["attribution_method"]; ok {
		t.Errorf("attribution_method should be omitted when empty")
	}
	if ev.SessionID != "" {
		t.Errorf("SessionID should be empty for unattributed")
	}
}

func TestResolveSession(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	// No pointer yet → unattributed, no error.
	id, _, method, err := ResolveSession(home, repo, now, time.Hour)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id != "" || method != "" {
		t.Errorf("expected empty, got id=%q method=%q", id, method)
	}

	// Write a fresh pointer.
	path, _ := session.ProjectPointerPath(home, repo)
	if err := session.WriteActive(path, session.ActivePointer{
		SessionID: "shp_in_repo", Label: "test", StartedAt: now.Add(-5 * time.Minute), LastActivity: now.Add(-1 * time.Minute),
	}); err != nil {
		t.Fatalf("seed pointer: %v", err)
	}
	id, label, method, err := ResolveSession(home, repo, now, time.Hour)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if id != "shp_in_repo" || method != "explicit" || label != "test" {
		t.Errorf("Resolve: id=%q label=%q method=%q", id, label, method)
	}

	// Stale pointer → unattributed.
	if err := session.WriteActive(path, session.ActivePointer{
		SessionID: "shp_old", LastActivity: now.Add(-10 * time.Hour),
	}); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	id, _, method, _ = ResolveSession(home, repo, now, time.Hour)
	if id != "" || method != "" {
		t.Errorf("expected stale → unattributed, got id=%q method=%q", id, method)
	}
}

func TestCollectCommitMetadataInRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	repo := t.TempDir()
	mustRun(t, repo, "git", "init", "-q", "-b", "main")
	mustRun(t, repo, "git", "config", "user.email", "test@example.com")
	mustRun(t, repo, "git", "config", "user.name", "Test User")
	mustRun(t, repo, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mustRun(t, repo, "git", "add", "a.txt")
	mustRun(t, repo, "git", "commit", "-qm", "feat: first commit")

	root, err := FindRepoRoot(repo)
	if err != nil {
		t.Fatalf("FindRepoRoot: %v", err)
	}
	// macOS resolves /var → /private/var; compare after EvalSymlinks.
	repoEval, _ := filepath.EvalSymlinks(repo)
	rootEval, _ := filepath.EvalSymlinks(root)
	if repoEval != rootEval {
		t.Errorf("root mismatch: got %q want %q", root, repo)
	}
	m, err := CollectCommitMetadata(repo)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if m.Subject != "feat: first commit" {
		t.Errorf("Subject: %q", m.Subject)
	}
	if !strings.HasPrefix(m.SHA, m.ShortSHA) {
		t.Errorf("SHA/Short mismatch: %q / %q", m.SHA, m.ShortSHA)
	}
	if m.Author != "Test User" {
		t.Errorf("Author: %q", m.Author)
	}
	if len(m.FilesTouched) != 1 || m.FilesTouched[0] != "a.txt" {
		t.Errorf("FilesTouched: %v", m.FilesTouched)
	}
	if m.FilesChanged != 1 || m.Insertions != 2 {
		t.Errorf("magnitude: %+v", m)
	}
}

func TestFindRepoRootOutsideGitErrors(t *testing.T) {
	if _, err := FindRepoRoot(t.TempDir()); err == nil {
		t.Errorf("expected error outside git repo")
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v (%s)", name, strings.Join(args, " "), err, out)
	}
}
