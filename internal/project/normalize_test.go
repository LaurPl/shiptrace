package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalize_Empty(t *testing.T) {
	if got := Normalize(""); got != "" {
		t.Errorf("Normalize(\"\") = %q, want \"\"", got)
	}
}

func TestNormalize_PlainPath(t *testing.T) {
	cases := []struct {
		cwd  string
		want string
	}{
		{"/home/user/projects/myproject", "myproject"},
		{"/home/user/projects/myproject/subdir", "subdir"},
		{"myproject", "myproject"},
		{"/", string(filepath.Separator)},
	}
	for _, tc := range cases {
		if got := Normalize(tc.cwd); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

func TestNormalize_ClaudeWorktree(t *testing.T) {
	cases := []struct {
		cwd  string
		want string
	}{
		{"/home/u/01_Shiptrace/.claude/worktrees/jovial-rubin-687768", "01_Shiptrace"},
		{"/home/u/01_Shiptrace/.claude/worktrees/jovial-rubin-687768/internal/x", "01_Shiptrace"},
		{"/a/b/c/proj/.claude/worktrees/foo", "proj"},
	}
	for _, tc := range cases {
		if got := Normalize(tc.cwd); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

func TestNormalize_ClaudeWorktree_NotAtSegmentBoundary(t *testing.T) {
	// A directory literally named ".claudeXworktrees" or similar must not
	// trigger the rule — only the exact segment boundary matches.
	cwd := "/home/u/proj-claude-worktrees-something/file"
	got := Normalize(cwd)
	if got != "file" {
		t.Errorf("Normalize(%q) = %q, want \"file\" (no false positive)", cwd, got)
	}
}

func TestNormalize_GitWorktreeFile(t *testing.T) {
	// Set up: a worktree directory with a .git *file* whose content
	// points to /<repo>/.git/worktrees/<name>.
	tmp := t.TempDir()
	worktree := filepath.Join(tmp, "external-worktree-dir")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	repo := filepath.Join(tmp, "myrepo")
	gitdirContent := "gitdir: " + filepath.Join(repo, ".git", "worktrees", "feature-branch") + "\n"
	if err := os.WriteFile(gitFile, []byte(gitdirContent), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	if got := Normalize(worktree); got != "myrepo" {
		t.Errorf("Normalize(%q) = %q, want \"myrepo\"", worktree, got)
	}
}

func TestNormalize_GitWorktreeFile_FromSubdir(t *testing.T) {
	tmp := t.TempDir()
	worktree := filepath.Join(tmp, "wt")
	subdir := filepath.Join(worktree, "deep", "nested", "dir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	repo := filepath.Join(tmp, "parent-repo")
	content := "gitdir: " + filepath.Join(repo, ".git", "worktrees", "foo") + "\n"
	if err := os.WriteFile(gitFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write .git: %v", err)
	}
	if got := Normalize(subdir); got != "parent-repo" {
		t.Errorf("Normalize(%q) = %q, want \"parent-repo\"", subdir, got)
	}
}

func TestNormalize_GitDir_NotAWorktreeFile(t *testing.T) {
	// A regular .git *directory* (the main repo case) is not a worktree
	// pointer — fall through to filepath.Base(cwd).
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "regular-repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if got := Normalize(repo); got != "regular-repo" {
		t.Errorf("Normalize(%q) = %q, want \"regular-repo\"", repo, got)
	}
}

func TestNormalize_GitFile_MalformedContent(t *testing.T) {
	// A .git file that doesn't start with "gitdir: " or whose gitdir
	// doesn't match the worktree marker → fall through to filepath.Base.
	tmp := t.TempDir()
	wt := filepath.Join(tmp, "weird")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("not a gitdir pointer\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := Normalize(wt); got != "weird" {
		t.Errorf("Normalize(%q) = %q, want \"weird\"", wt, got)
	}
}

func TestNormalize_ClaudeBeatsGit(t *testing.T) {
	// If both signals are present, the Claude worktree rule wins because
	// it's the higher-confidence match (pure path, no I/O).
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "outerproj")
	wt := filepath.Join(proj, ".claude", "worktrees", "abc-def-123")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Also drop a misleading .git file in wt.
	bogus := "gitdir: " + filepath.Join(tmp, "wrong-repo", ".git", "worktrees", "x") + "\n"
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte(bogus), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := Normalize(wt); got != "outerproj" {
		t.Errorf("Normalize(%q) = %q, want \"outerproj\" (Claude rule wins)", wt, got)
	}
}
