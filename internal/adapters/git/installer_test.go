package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newRepoSkeleton(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git", "hooks"), 0o755); err != nil {
		t.Fatalf("mkdir .git/hooks: %v", err)
	}
	return repo
}

func TestInstallFreshHook(t *testing.T) {
	repo := newRepoSkeleton(t)
	changed, err := InstallPostCommit(repo, "/usr/local/bin/shiptrace-git-postcommit")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true on fresh install")
	}
	hookPath := filepath.Join(repo, ".git", "hooks", HookFileName)
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	contents := string(data)
	for _, must := range []string{"#!/bin/sh", HookMarker, "shiptrace-git-postcommit", "command -v"} {
		if !strings.Contains(contents, must) {
			t.Errorf("hook missing %q:\n%s", must, contents)
		}
	}
	info, _ := os.Stat(hookPath)
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("hook should be executable, got mode %v", info.Mode())
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	repo := newRepoSkeleton(t)
	binary := "/usr/local/bin/shiptrace-git-postcommit"
	if _, err := InstallPostCommit(repo, binary); err != nil {
		t.Fatalf("first: %v", err)
	}
	changed, err := InstallPostCommit(repo, binary)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if changed {
		t.Errorf("expected idempotent re-install (changed=false)")
	}
}

func TestInstallUpdatesBinaryPath(t *testing.T) {
	repo := newRepoSkeleton(t)
	_, _ = InstallPostCommit(repo, "/old/path/shiptrace-git-postcommit")
	changed, err := InstallPostCommit(repo, "/new/path/shiptrace-git-postcommit")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !changed {
		t.Errorf("expected change when binary path differs")
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "hooks", HookFileName))
	if strings.Contains(string(data), "/old/path") {
		t.Errorf("old path should be gone, got:\n%s", data)
	}
	if !strings.Contains(string(data), "/new/path") {
		t.Errorf("new path missing, got:\n%s", data)
	}
}

func TestInstallPreservesUserContent(t *testing.T) {
	repo := newRepoSkeleton(t)
	preexisting := "#!/bin/sh\necho 'user hook'\nexit 0\n"
	hookPath := filepath.Join(repo, ".git", "hooks", HookFileName)
	if err := os.WriteFile(hookPath, []byte(preexisting), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := InstallPostCommit(repo, "/path/shiptrace-git-postcommit"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, _ := os.ReadFile(hookPath)
	if !strings.Contains(string(data), "echo 'user hook'") {
		t.Errorf("user content lost, got:\n%s", data)
	}
}

func TestUninstallRemovesOurBlock(t *testing.T) {
	repo := newRepoSkeleton(t)
	preexisting := "#!/bin/sh\necho 'user hook'\nexit 0\n"
	hookPath := filepath.Join(repo, ".git", "hooks", HookFileName)
	_ = os.WriteFile(hookPath, []byte(preexisting), 0o755)
	_, _ = InstallPostCommit(repo, "/path/shiptrace-git-postcommit")

	changed, err := UninstallPostCommit(repo)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !changed {
		t.Errorf("expected changed=true")
	}
	data, _ := os.ReadFile(hookPath)
	if strings.Contains(string(data), "shiptrace") {
		t.Errorf("our block remained:\n%s", data)
	}
	if !strings.Contains(string(data), "echo 'user hook'") {
		t.Errorf("user content lost:\n%s", data)
	}
}

func TestUninstallRemovesShebangOnlyFile(t *testing.T) {
	repo := newRepoSkeleton(t)
	_, _ = InstallPostCommit(repo, "/path/shiptrace-git-postcommit")
	changed, _ := UninstallPostCommit(repo)
	if !changed {
		t.Errorf("expected changed=true")
	}
	hookPath := filepath.Join(repo, ".git", "hooks", HookFileName)
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected hook file removed when only shebang remains, err=%v", err)
	}
}

func TestUninstallMissingHookIsNoOp(t *testing.T) {
	repo := newRepoSkeleton(t)
	changed, err := UninstallPostCommit(repo)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if changed {
		t.Errorf("expected changed=false for missing hook")
	}
}

func TestIsInstalled(t *testing.T) {
	repo := newRepoSkeleton(t)
	if installed, _, _ := IsInstalled(repo); installed {
		t.Errorf("fresh repo should report not installed")
	}
	_, _ = InstallPostCommit(repo, "/some/path/shiptrace-git-postcommit")
	installed, bin, _ := IsInstalled(repo)
	if !installed {
		t.Errorf("expected installed=true")
	}
	if bin != "/some/path/shiptrace-git-postcommit" {
		t.Errorf("binary path: %q", bin)
	}
}

func TestInstallRejectsNonRepo(t *testing.T) {
	if _, err := InstallPostCommit(t.TempDir(), "/path/x"); err == nil {
		t.Errorf("expected error when .git/hooks missing")
	}
}

// TestInstallQuotesShellHostilePaths confirms that paths containing $,
// backtick, single quotes, or spaces don't cause /bin/sh to expand them when
// the hook runs. We pass `sh -n` (syntax-only) to assert the script parses,
// and round-trip through IsInstalled to confirm we can recover the path.
func TestInstallQuotesShellHostilePaths(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	hostile := []string{
		"/usr/local/bin/shiptrace-git-postcommit",
		`/tmp/path with space/shiptrace-git-postcommit`,
		`/tmp/$(echo evil)/shiptrace-git-postcommit`,
		"/tmp/`whoami`/shiptrace-git-postcommit",
		"/tmp/has'single'quote/shiptrace-git-postcommit",
	}
	for _, p := range hostile {
		t.Run(p, func(t *testing.T) {
			repo := newRepoSkeleton(t)
			if _, err := InstallPostCommit(repo, p); err != nil {
				t.Fatalf("install: %v", err)
			}
			hookPath := filepath.Join(repo, ".git", "hooks", HookFileName)
			// Syntax-check the generated hook with `sh -n`.
			if out, err := exec.Command("sh", "-n", hookPath).CombinedOutput(); err != nil {
				body, _ := os.ReadFile(hookPath)
				t.Fatalf("sh -n failed: %v\noutput: %s\nhook:\n%s", err, out, body)
			}
			// Round-trip through IsInstalled.
			installed, recovered, err := IsInstalled(repo)
			if err != nil {
				t.Fatalf("IsInstalled: %v", err)
			}
			if !installed {
				t.Errorf("expected installed=true")
			}
			if recovered != p {
				t.Errorf("path round-trip:\n  set: %q\n  got: %q", p, recovered)
			}
		})
	}
}
