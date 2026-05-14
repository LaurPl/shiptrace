package display

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/attrib"
)

func plain() Color { return NewColor(false) }

func TestSessionStartedLine(t *testing.T) {
	var buf bytes.Buffer
	SessionStarted(&buf, plain(), "shp_abc", "writing slides")
	out := buf.String()
	for _, want := range []string{"✓", "Session started:", `"writing slides"`, "shp_abc"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestSessionStoppedLine(t *testing.T) {
	var buf bytes.Buffer
	started := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	ended := started.Add(38 * time.Second)
	SessionStopped(&buf, plain(), "shp_abc", "smoke test", started, ended)
	out := buf.String()
	if !strings.Contains(out, "duration 38s") {
		t.Errorf("expected duration formatting, got: %s", out)
	}
}

func TestShipResultUnattributed(t *testing.T) {
	var buf bytes.Buffer
	r := &attrib.Resolution{Source: attrib.SourceNone}
	ShipResult(&buf, plain(), r, time.Now())
	out := buf.String()
	if !strings.Contains(out, "⚠") || !strings.Contains(out, "unattributed") {
		t.Errorf("expected warning + 'unattributed', got: %s", out)
	}
}

func TestShipResultFromPointer(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	r := &attrib.Resolution{
		SessionID: "shp_xyz",
		Label:     "Easter workshop slides",
		StartedAt: now.Add(-2 * time.Hour),
		Source:    attrib.SourcePointer,
	}
	ShipResult(&buf, plain(), r, now)
	out := buf.String()
	for _, want := range []string{"✓", "Ship event logged", "Easter workshop slides", "shp_xyz", "started 2h ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %s", want, out)
		}
	}
}

func TestShipResultConflictPrefix(t *testing.T) {
	var buf bytes.Buffer
	r := &attrib.Resolution{
		SessionID: "shp_aaa",
		Source:    attrib.SourceFlag,
		Conflict:  &attrib.Conflict{LosingSource: attrib.SourceEnv, LosingSessionID: "shp_bbb"},
	}
	ShipResult(&buf, plain(), r, time.Now())
	out := buf.String()
	if !strings.Contains(out, "Session conflict") {
		t.Errorf("expected conflict notice, got: %s", out)
	}
	if !strings.Contains(out, "shp_aaa") || !strings.Contains(out, "shp_bbb") {
		t.Errorf("conflict line should mention both IDs, got: %s", out)
	}
}

func TestNoColorOutputHasNoAnsi(t *testing.T) {
	var buf bytes.Buffer
	SessionStarted(&buf, plain(), "shp_abc", "x")
	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("plain Color leaked ANSI escapes: %q", buf.String())
	}
}
