package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWithDefaultsSetsSchemaVersionAndTs(t *testing.T) {
	e := Event{EventType: SessionStart}.WithDefaults()
	if e.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion: got %q, want %q", e.SchemaVersion, SchemaVersion)
	}
	if e.Ts.IsZero() {
		t.Fatalf("Ts should be populated by WithDefaults")
	}
	if e.Ts.Location() != time.UTC {
		t.Fatalf("Ts should be UTC, got %v", e.Ts.Location())
	}
}

func TestWithDefaultsPreservesProvidedFields(t *testing.T) {
	custom := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e := Event{
		SchemaVersion: "9",
		EventType:     ToolUse,
		Ts:            custom,
	}.WithDefaults()
	if e.SchemaVersion != "9" {
		t.Fatalf("SchemaVersion overwritten: %q", e.SchemaVersion)
	}
	if !e.Ts.Equal(custom) {
		t.Fatalf("Ts overwritten: %v", e.Ts)
	}
}

func TestRoundTripAllEventTypes(t *testing.T) {
	cases := []EventType{SessionStart, SessionStop, Prompt, ToolUse, ReplanSignal, Ship}
	for _, k := range cases {
		t.Run(string(k), func(t *testing.T) {
			orig := Event{
				EventType:    k,
				SessionID:    "shp_abc123",
				Provider:     "manual",
				Project:      "shiptrace",
				Label:        "test event",
				FilesTouched: []string{"a/b.go", "c.md"},
				Tokens:       &TokenCount{In: 10, Out: 20},
				Metadata:     map[string]any{"kind": "manual"},
			}.WithDefaults()

			data, err := json.Marshal(orig)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(data), `"schema_version":"1"`) {
				t.Fatalf("schema_version missing from JSON: %s", data)
			}

			var back Event
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.EventType != orig.EventType {
				t.Errorf("EventType: got %q, want %q", back.EventType, orig.EventType)
			}
			if back.SessionID != orig.SessionID {
				t.Errorf("SessionID: got %q, want %q", back.SessionID, orig.SessionID)
			}
			if back.Label != orig.Label {
				t.Errorf("Label: got %q, want %q", back.Label, orig.Label)
			}
			if back.Tokens == nil || back.Tokens.In != 10 || back.Tokens.Out != 20 {
				t.Errorf("Tokens round-trip failed: %+v", back.Tokens)
			}
			if len(back.FilesTouched) != 2 {
				t.Errorf("FilesTouched: got %v", back.FilesTouched)
			}
		})
	}
}

func TestOptionalFieldsOmittedWhenEmpty(t *testing.T) {
	e := Event{EventType: SessionStart}.WithDefaults()
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{`"session_id"`, `"provider"`, `"project"`, `"label"`, `"tokens"`, `"files_touched"`} {
		if strings.Contains(string(data), banned) {
			t.Errorf("expected %s to be omitted, got: %s", banned, data)
		}
	}
}
