package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeUsesEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(HomeEnv, tmp)

	h, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if h != tmp {
		t.Fatalf("Home: got %q, want %q", h, tmp)
	}

	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("Home should ensure a directory")
	}
}

func TestEventsDirCreated(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(HomeEnv, tmp)

	d, err := EventsDir()
	if err != nil {
		t.Fatalf("EventsDir: %v", err)
	}
	if !strings.HasPrefix(d, tmp) {
		t.Fatalf("EventsDir not under tmp: %q", d)
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatalf("events dir not created: %v", err)
	}
}

func TestDBPathPointerCheckpointDoNotCreateFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(HomeEnv, tmp)

	for name, fn := range map[string]func() (string, error){
		"DBPath":         DBPath,
		"PointerPath":    PointerPath,
		"CheckpointPath": CheckpointPath,
	} {
		t.Run(name, func(t *testing.T) {
			p, err := fn()
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !strings.HasPrefix(p, tmp) {
				t.Fatalf("%s not under tmp: %q", name, p)
			}
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("%s should not create the file (stat err: %v)", name, err)
			}
		})
	}
}

func TestHomeWithoutOverrideUsesUserHome(t *testing.T) {
	// Use a fresh HOME so the test doesn't pollute the real ~/.shiptrace dir.
	fake := t.TempDir()
	t.Setenv("HOME", fake)
	t.Setenv(HomeEnv, "")

	h, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	want := filepath.Join(fake, ".shiptrace")
	if h != want {
		t.Fatalf("Home: got %q, want %q", h, want)
	}
}
