// Package config loads ~/.shiptrace/config.yaml. The schema covers only the
// fields shiptrace v0.1 reads — ship_paths and exploration mode per project.
// Fields the design doc mentions for later releases (adapters, privacy
// overrides, retention) are intentionally not surfaced yet so we don't pin
// down their shape prematurely.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// FileName is the basename of the config file under SHIPTRACE_HOME.
const FileName = "config.yaml"

// Mode controls whether a project is expected to ship.
type Mode string

const (
	// ModeProduction is the default — sessions in this project should
	// produce ships; sessions-to-ship ratio is meaningful.
	ModeProduction Mode = "production"
	// ModeExploration tags projects where zero ships is a successful
	// outcome (e.g. research notes). Day-5 dashboard renders these
	// differently to avoid Goodhart's-Law incentives.
	ModeExploration Mode = "exploration"
)

// Project is the per-project configuration block.
type Project struct {
	Paths     []string `yaml:"paths,omitempty"`
	ShipPaths []string `yaml:"ship_paths,omitempty"`
	Mode      Mode     `yaml:"mode,omitempty"`
}

// Config is the top-level structure.
type Config struct {
	Projects map[string]Project `yaml:"projects,omitempty"`
}

// Load reads the YAML file at path. A missing file returns an empty
// Config — that's a valid state (the user hasn't configured anything yet),
// not an error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return &c, nil
}

// ProjectByPath returns the (name, Project) pair whose `paths` covers cwd,
// or ("", Project{}, false) when no project matches. Useful for project
// auto-detection from a working directory.
func (c *Config) ProjectByPath(cwd string) (string, Project, bool) {
	if c == nil || cwd == "" {
		return "", Project{}, false
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", Project{}, false
	}
	for name, p := range c.Projects {
		for _, root := range p.Paths {
			absRoot, err := filepath.Abs(root)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(absRoot, absCwd)
			if err != nil {
				continue
			}
			if rel == "." || (rel != "" && rel != ".." && !startsWithDotDot(rel)) {
				return name, p, true
			}
		}
	}
	return "", Project{}, false
}

func startsWithDotDot(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}

// AllShipPaths returns the concatenated ship_paths across every project,
// preserving order. Used by the FS adapter to know what to watch when no
// project filter is set.
func (c *Config) AllShipPaths() []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, p := range c.Projects {
		out = append(out, p.ShipPaths...)
	}
	return out
}
