package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HookMarker is a magic string we embed in any post-commit hook we install
// so subsequent runs can detect and update only our portion without
// touching unrelated lines.
const HookMarker = "# shiptrace-post-commit (managed)"

// HookFileName is the filename git expects under .git/hooks/.
const HookFileName = "post-commit"

// InstallPostCommit writes a tiny shell hook to <repoRoot>/.git/hooks/post-commit
// that invokes shiptrace-git-postcommit. The function is idempotent: an
// existing managed block is replaced with the new path; user-authored
// content above and below the markers is preserved.
//
// Returns true when the file was actually modified, false when nothing
// changed (idempotent re-install with the same binary path).
func InstallPostCommit(repoRoot, binaryPath string) (changed bool, err error) {
	if repoRoot == "" || binaryPath == "" {
		return false, errors.New("git installer: repoRoot and binaryPath required")
	}
	hookDir := filepath.Join(repoRoot, ".git", "hooks")
	if _, err := os.Stat(hookDir); err != nil {
		return false, fmt.Errorf("git installer: %s not found — is this a git repo?", hookDir)
	}
	hookPath := filepath.Join(hookDir, HookFileName)

	existing, _ := os.ReadFile(hookPath)
	desiredBlock := buildManagedBlock(binaryPath)
	newContents := mergeManagedBlock(string(existing), desiredBlock)
	if newContents == string(existing) {
		return false, nil
	}

	// Atomic write to avoid leaving a half-written hook behind.
	tmp := hookPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newContents), 0o755); err != nil {
		return false, fmt.Errorf("git installer: write tmp: %w", err)
	}
	if err := os.Rename(tmp, hookPath); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("git installer: rename: %w", err)
	}
	return true, nil
}

// UninstallPostCommit removes shiptrace's managed block from the
// post-commit hook. The rest of the file is preserved; if the file ends
// up empty (just a shebang), it is removed entirely.
func UninstallPostCommit(repoRoot string) (changed bool, err error) {
	hookPath := filepath.Join(repoRoot, ".git", "hooks", HookFileName)
	existing, err := os.ReadFile(hookPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	stripped := stripManagedBlock(string(existing))
	if stripped == string(existing) {
		return false, nil
	}
	if strings.TrimSpace(stripped) == "" || strings.TrimSpace(stripped) == "#!/bin/sh" {
		if err := os.Remove(hookPath); err != nil {
			return false, err
		}
		return true, nil
	}
	tmp := hookPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(stripped), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, hookPath); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// IsInstalled reports whether the managed shiptrace block is present in
// the post-commit hook for repoRoot.
func IsInstalled(repoRoot string) (bool, string, error) {
	hookPath := filepath.Join(repoRoot, ".git", "hooks", HookFileName)
	data, err := os.ReadFile(hookPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "", nil
		}
		return false, "", err
	}
	contents := string(data)
	if !strings.Contains(contents, HookMarker) {
		return false, "", nil
	}
	// Pull the binary path out of the managed block so doctor can show it.
	binPath := extractBinaryFromBlock(contents)
	return true, binPath, nil
}

const (
	blockStartMarker = HookMarker + " — start"
	blockEndMarker   = HookMarker + " — end"
)

func buildManagedBlock(binaryPath string) string {
	// `command -v ... > /dev/null` short-circuits when the binary moves
	// away, and `|| true` ensures a missing recorder never fails the
	// user's commit.
	return fmt.Sprintf(
		`%s
if command -v %q >/dev/null 2>&1; then
  %s || true
elif [ -x %q ]; then
  %q || true
fi
%s
`,
		blockStartMarker,
		filepath.Base(binaryPath),
		filepath.Base(binaryPath),
		binaryPath,
		binaryPath,
		blockEndMarker,
	)
}

// mergeManagedBlock replaces any existing managed block; if the file has
// no shebang, we add one; if there's no managed block yet, the new block
// is appended.
func mergeManagedBlock(existing, block string) string {
	if existing == "" {
		return "#!/bin/sh\n\n" + block
	}
	if startIdx := strings.Index(existing, blockStartMarker); startIdx >= 0 {
		endIdx := strings.Index(existing[startIdx:], blockEndMarker)
		if endIdx >= 0 {
			endIdx = startIdx + endIdx + len(blockEndMarker) + 1 // include trailing newline
			if endIdx > len(existing) {
				endIdx = len(existing)
			}
			return existing[:startIdx] + block + existing[endIdx:]
		}
	}
	// No managed block yet — append.
	if !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	if !strings.Contains(existing, "#!") {
		return "#!/bin/sh\n\n" + existing + block
	}
	return existing + "\n" + block
}

func stripManagedBlock(existing string) string {
	startIdx := strings.Index(existing, blockStartMarker)
	if startIdx < 0 {
		return existing
	}
	endIdx := strings.Index(existing[startIdx:], blockEndMarker)
	if endIdx < 0 {
		return existing
	}
	endIdx = startIdx + endIdx + len(blockEndMarker)
	// Eat the trailing newline if present.
	if endIdx < len(existing) && existing[endIdx] == '\n' {
		endIdx++
	}
	// Trim a single leading blank line if the strip created one.
	out := existing[:startIdx] + existing[endIdx:]
	out = strings.TrimRight(out, "\n") + "\n"
	return out
}

func extractBinaryFromBlock(contents string) string {
	startIdx := strings.Index(contents, blockStartMarker)
	endIdx := strings.Index(contents, blockEndMarker)
	if startIdx < 0 || endIdx < 0 || startIdx > endIdx {
		return ""
	}
	block := contents[startIdx:endIdx]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		// Match the absolute-path arm: `elif [ -x "/full/path" ]; then`
		if strings.HasPrefix(line, "elif [ -x ") {
			// Cheap parse — quote-delimited path between the two `"`.
			startQ := strings.Index(line, `"`)
			endQ := strings.LastIndex(line, `"`)
			if startQ >= 0 && endQ > startQ {
				return line[startQ+1 : endQ]
			}
		}
	}
	return ""
}
