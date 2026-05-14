package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectKeyStable(t *testing.T) {
	tmp := t.TempDir()
	k1, err := ProjectKey(tmp)
	if err != nil {
		t.Fatalf("ProjectKey: %v", err)
	}
	k2, err := ProjectKey(tmp)
	if err != nil {
		t.Fatalf("ProjectKey: %v", err)
	}
	if k1 != k2 {
		t.Fatalf("ProjectKey not stable: %q vs %q", k1, k2)
	}
	if len(k1) != 16 {
		t.Errorf("key length: %d want 16", len(k1))
	}
}

func TestProjectKeyDiffersAcrossPaths(t *testing.T) {
	k1, _ := ProjectKey(t.TempDir())
	k2, _ := ProjectKey(t.TempDir())
	if k1 == k2 {
		t.Errorf("two different temp dirs produced same key: %q", k1)
	}
}

func TestProjectKeyResolvesToGitRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	sub := filepath.Join(root, "sub", "deeper")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	kRoot, _ := ProjectKey(root)
	kSub, _ := ProjectKey(sub)
	if kRoot != kSub {
		t.Fatalf("expected subdir to map to repo root: rootKey=%q subKey=%q", kRoot, kSub)
	}
}

func TestProjectPointerPath(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	path, err := ProjectPointerPath(home, cwd)
	if err != nil {
		t.Fatalf("ProjectPointerPath: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join(home, ProjectPointersDirName)) {
		t.Errorf("path not under project-pointers/: %s", path)
	}
	if !strings.HasSuffix(path, ".json") {
		t.Errorf("path lacks .json suffix: %s", path)
	}
}
