package claudecode

import (
	"strings"
	"testing"
)

func TestParsePayloadKnownFields(t *testing.T) {
	raw := `{
		"session_id": "cc-abc-123",
		"transcript_path": "/Users/x/.claude/projects/foo/cc-abc-123.jsonl",
		"cwd": "/Users/x/work/shiptrace",
		"hook_event_name": "SessionStart",
		"model": "claude-opus-4-7",
		"permission_mode": "default"
	}`
	p, err := ParsePayload(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.SessionID != "cc-abc-123" {
		t.Errorf("SessionID: %q", p.SessionID)
	}
	if p.Cwd == "" || p.Model == "" || p.HookEventName == "" {
		t.Errorf("expected fields populated: %+v", p)
	}
	if len(p.Extras) != 0 {
		t.Errorf("unexpected extras: %+v", p.Extras)
	}
}

func TestParsePayloadPreservesUnknownExtras(t *testing.T) {
	raw := `{
		"session_id": "cc-abc",
		"future_field": "whatever",
		"another": {"nested": true}
	}`
	p, err := ParsePayload(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if _, ok := p.Extras["future_field"]; !ok {
		t.Errorf("future_field missing from Extras: %+v", p.Extras)
	}
	if _, ok := p.Extras["another"]; !ok {
		t.Errorf("another missing from Extras")
	}
	if _, ok := p.Extras["session_id"]; ok {
		t.Errorf("known field leaked into Extras")
	}
}

func TestParsePayloadToolUse(t *testing.T) {
	raw := `{
		"session_id": "cc-xyz",
		"hook_event_name": "PostToolUse",
		"tool_name": "Edit",
		"tool_input": {"file_path": "/x/y.go", "old_string": "a", "new_string": "b"}
	}`
	p, err := ParsePayload(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.ToolName != "Edit" {
		t.Errorf("ToolName: %q", p.ToolName)
	}
	if len(p.ToolInput) == 0 {
		t.Errorf("ToolInput empty: %s", p.ToolInput)
	}
}

func TestParsePayloadEmptyAllowed(t *testing.T) {
	p, err := ParsePayload(strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if p.SessionID != "" {
		t.Errorf("expected empty SessionID")
	}
}

func TestParsePayloadInvalidJSON(t *testing.T) {
	if _, err := ParsePayload(strings.NewReader(`not json`)); err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
}

// TestParsePayloadRefusesOversizedInput confirms the MaxPayloadBytes cap is
// enforced. We synthesise a JSON object whose total size exceeds the cap by
// padding a single value with spaces — cheap, deterministic.
func TestParsePayloadRefusesOversizedInput(t *testing.T) {
	prefix := `{"session_id":"cc-big","prompt":"`
	suffix := `"}`
	// Total body must exceed MaxPayloadBytes by at least one byte.
	padLen := MaxPayloadBytes - len(prefix) - len(suffix) + 1
	if padLen <= 0 {
		t.Fatalf("padding length non-positive; cap=%d", MaxPayloadBytes)
	}
	body := prefix + strings.Repeat("a", padLen) + suffix
	if _, err := ParsePayload(strings.NewReader(body)); err == nil {
		t.Fatalf("expected error for payload exceeding %d bytes", MaxPayloadBytes)
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("unexpected error: %v", err)
	}
}
