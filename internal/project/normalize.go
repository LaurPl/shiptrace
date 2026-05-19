// Package project maps a working-directory path to the canonical project
// name shiptrace reports against. Worktree directories — both Claude
// Code's `.claude/worktrees/<name>` convention and git's `git worktree
// add` pointer files — resolve to the parent project's basename rather
// than the worktree directory's own name.
//
// Without this normalization every worktree shows up as its own
// "project" in the dashboard, polluting the distribution and replan
// views with one-off rows that aren't really projects.
package project

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Normalize returns the canonical project name for cwd.
//
// Detection rules, in order:
//
//  1. ".claude/worktrees/<name>": basename of the directory that contains
//     ".claude". Pure path manipulation, no I/O.
//  2. ".git" *file* (not directory) at cwd or in a parent up to 64 levels,
//     whose first line is "gitdir: <repo>/.git/worktrees/<name>": basename
//     of <repo>. Best-effort; returns "" silently on any I/O error.
//  3. Otherwise: filepath.Base(cwd).
//
// Empty cwd returns "".
func Normalize(cwd string) string {
	if cwd == "" {
		return ""
	}
	cleaned := filepath.Clean(cwd)
	if name := normalizeClaudeWorktree(cleaned); name != "" {
		return name
	}
	if name := normalizeGitWorktree(cleaned); name != "" {
		return name
	}
	return filepath.Base(cleaned)
}

func normalizeClaudeWorktree(cleaned string) string {
	sep := string(filepath.Separator)
	needle := sep + ".claude" + sep + "worktrees" + sep
	idx := strings.Index(cleaned, needle)
	if idx < 0 {
		return ""
	}
	parent := cleaned[:idx]
	if parent == "" {
		return ""
	}
	return filepath.Base(parent)
}

func normalizeGitWorktree(cleaned string) string {
	sep := string(filepath.Separator)
	marker := sep + ".git" + sep + "worktrees" + sep
	cur := cleaned
	for i := 0; i < 64; i++ {
		gitPath := filepath.Join(cur, ".git")
		info, err := os.Stat(gitPath)
		if err == nil && !info.IsDir() {
			repo := readGitdirParent(gitPath, marker)
			if repo != "" {
				return filepath.Base(repo)
			}
			return ""
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return ""
}

func readGitdirParent(gitFilePath, marker string) string {
	f, err := os.Open(gitFilePath)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return ""
	}
	first := scanner.Text()
	const prefix = "gitdir: "
	if !strings.HasPrefix(first, prefix) {
		return ""
	}
	gitdir := strings.TrimPrefix(first, prefix)
	mi := strings.Index(gitdir, marker)
	if mi < 0 {
		return ""
	}
	return gitdir[:mi]
}
