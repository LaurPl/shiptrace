package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProjectPointersDirName is the subdir under SHIPTRACE_HOME where per-project
// pointers live. We never write into the user's own repo to keep our footprint
// invisible to git status; a stable hash-of-abspath maps cwd → pointer file.
const ProjectPointersDirName = "project-pointers"

// ProjectKey returns a stable identifier for the project rooted at cwd.
// Preference order:
//  1. The git repo root (so subdirs of the same repo share a session)
//  2. The cwd itself (for non-git directories, e.g. obsidian vaults)
//
// The returned key is the first 16 hex chars of sha256(abspath). 16 hex
// chars ≈ 64 bits of entropy — more than enough to avoid collisions across
// the few dozen projects a single user typically tracks.
func ProjectKey(cwd string) (string, error) {
	root, err := resolveProjectRoot(cwd)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("session: abspath %s: %w", root, err)
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:16], nil
}

// ProjectPointerPath returns the absolute path to the per-project pointer
// file for cwd, given the resolved shiptrace home dir.
func ProjectPointerPath(home, cwd string) (string, error) {
	key, err := ProjectKey(cwd)
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ProjectPointersDirName, key+".json"), nil
}

// resolveProjectRoot returns the git toplevel for cwd, or cwd if the
// directory isn't tracked by git. We shell out instead of reimplementing
// `git rev-parse --show-toplevel` so we never disagree with git's own
// notion of repo boundaries (worktrees, submodules, etc.).
func resolveProjectRoot(cwd string) (string, error) {
	if cwd == "" {
		return "", errors.New("session: empty cwd")
	}
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err == nil {
		root := strings.TrimSpace(string(out))
		if root != "" {
			return root, nil
		}
	}
	// Not in a git repo, or git missing — fall back to cwd. Both are fine.
	return cwd, nil
}
