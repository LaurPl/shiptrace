package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitInstallsHooksAndIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	settingsPath := filepath.Join(tmp, "settings.json")

	fakeHookBin := filepath.Join(tmp, "shiptrace-cc-hook")
	if err := os.WriteFile(fakeHookBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake hook bin: %v", err)
	}

	var out bytes.Buffer
	if err := runInit(strings.NewReader("y\n"), &out, true /* yes */, fakeHookBin, settingsPath); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	if !strings.Contains(out.String(), "Adding 6 hook entries") {
		t.Errorf("expected 'Adding 6 hook entries', got: %s", out.String())
	}

	// Second run should short-circuit.
	out.Reset()
	if err := runInit(strings.NewReader(""), &out, true, fakeHookBin, settingsPath); err != nil {
		t.Fatalf("runInit second pass: %v", err)
	}
	if !strings.Contains(out.String(), "already installed") {
		t.Errorf("expected idempotent message, got: %s", out.String())
	}
}

func TestRunInitPreservesUnrelatedSettings(t *testing.T) {
	tmp := t.TempDir()
	settingsPath := filepath.Join(tmp, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"theme":"dark","statusLine":{"type":"command"}}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	fakeHookBin := filepath.Join(tmp, "shiptrace-cc-hook")
	_ = os.WriteFile(fakeHookBin, []byte("#"), 0o755)

	var out bytes.Buffer
	if err := runInit(strings.NewReader("y\n"), &out, true, fakeHookBin, settingsPath); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	final, _ := os.ReadFile(settingsPath)
	for _, must := range []string{"theme", "dark", "statusLine", "hooks", "session-start"} {
		if !strings.Contains(string(final), must) {
			t.Errorf("final settings missing %q: %s", must, final)
		}
	}
}

func TestRunInitAbortsOnNo(t *testing.T) {
	tmp := t.TempDir()
	settingsPath := filepath.Join(tmp, "settings.json")
	fakeHookBin := filepath.Join(tmp, "shiptrace-cc-hook")
	_ = os.WriteFile(fakeHookBin, []byte("#"), 0o755)

	var out bytes.Buffer
	if err := runInit(strings.NewReader("n\n"), &out, false, fakeHookBin, settingsPath); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Errorf("settings.json should not be written when user says no")
	}
	if !strings.Contains(out.String(), "Aborted") {
		t.Errorf("expected 'Aborted' message: %s", out.String())
	}
}
