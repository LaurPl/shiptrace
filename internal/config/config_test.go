package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c == nil || len(c.Projects) != 0 {
		t.Errorf("expected empty config, got %+v", c)
	}
}

func TestLoadHappyPath(t *testing.T) {
	path := writeConfig(t, `
projects:
  social:
    paths:
      - /home/lau/projects/social
    ship_paths:
      - /home/lau/projects/social/scheduled/**
      - /home/lau/projects/social/published/**
    mode: production
  research:
    paths:
      - /home/lau/vaults/research
    mode: exploration
`)
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Projects) != 2 {
		t.Fatalf("projects: got %d want 2", len(c.Projects))
	}
	social := c.Projects["social"]
	if social.Mode != ModeProduction {
		t.Errorf("social.Mode: %q", social.Mode)
	}
	if len(social.ShipPaths) != 2 {
		t.Errorf("social.ShipPaths: %v", social.ShipPaths)
	}
	research := c.Projects["research"]
	if research.Mode != ModeExploration {
		t.Errorf("research.Mode: %q", research.Mode)
	}
}

func TestProjectByPath(t *testing.T) {
	tmp := t.TempDir()
	socialRoot := filepath.Join(tmp, "social")
	researchRoot := filepath.Join(tmp, "research")
	_ = os.MkdirAll(filepath.Join(socialRoot, "drafts"), 0o755)
	_ = os.MkdirAll(researchRoot, 0o755)
	c := &Config{Projects: map[string]Project{
		"social":   {Paths: []string{socialRoot}, Mode: ModeProduction},
		"research": {Paths: []string{researchRoot}, Mode: ModeExploration},
	}}

	cases := []struct {
		cwd      string
		wantName string
		wantOK   bool
	}{
		{socialRoot, "social", true},
		{filepath.Join(socialRoot, "drafts"), "social", true},
		{researchRoot, "research", true},
		{tmp, "", false},
	}
	for _, c2 := range cases {
		got, _, ok := c.ProjectByPath(c2.cwd)
		if ok != c2.wantOK || got != c2.wantName {
			t.Errorf("ProjectByPath(%q): got (%q, %v) want (%q, %v)", c2.cwd, got, ok, c2.wantName, c2.wantOK)
		}
	}
}

func TestAllShipPaths(t *testing.T) {
	c := &Config{Projects: map[string]Project{
		"a": {ShipPaths: []string{"/a/**"}},
		"b": {ShipPaths: []string{"/b/**", "/b2/**"}},
	}}
	all := c.AllShipPaths()
	if len(all) != 3 {
		t.Errorf("got %v want 3 entries", all)
	}
}

func TestLoadParseErrorBubbles(t *testing.T) {
	path := writeConfig(t, `projects: [not, a, map]`)
	if _, err := Load(path); err == nil {
		t.Errorf("expected parse error")
	}
}
