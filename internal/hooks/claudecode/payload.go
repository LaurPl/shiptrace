// Package claudecode is the recorder for Claude Code's native hook payloads.
// It is intentionally stdlib-only (no cobra, no sqlite, no fsnotify) because
// the shiptrace-cc-hook binary that consumes this package runs on the hot
// path of every prompt/tool-use and must stay under 30ms p99.
//
// Source of truth for the payload shape: Claude Code hook documentation.
// We treat unknown fields as forward-compat — we never reject a payload for
// having more than we expect.
package claudecode

import (
	"encoding/json"
	"io"
)

// HookPayload is the union of fields shiptrace cares about across hook
// events. Claude Code includes many more fields per event type; we
// intentionally only model the ones we materialize, and the raw Extras map
// preserves the rest for later analysis or schema drift.
type HookPayload struct {
	// SessionID is Claude Code's own session UUID. We never use it as a
	// primary key — see internal/hooks/claudecode/sessionmap.go for the
	// shp_ ↔ cc-uuid mapping.
	SessionID string `json:"session_id,omitempty"`

	// TranscriptPath points at CC's per-session transcript JSONL. We don't
	// parse it (per D5 — undocumented, version-volatile), but we capture
	// the path in metadata for debugging.
	TranscriptPath string `json:"transcript_path,omitempty"`

	// Cwd is the working directory CC was invoked from.
	Cwd string `json:"cwd,omitempty"`

	// HookEventName, when present, names the firing hook (SessionStart,
	// UserPromptSubmit, …). Not strictly required — the binary subcommand
	// already tells us — but useful for sanity-checking.
	HookEventName string `json:"hook_event_name,omitempty"`

	// Model and PermissionMode show up on SessionStart and similar events.
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`

	// Prompt is populated on UserPromptSubmit. We do NOT log this verbatim
	// by default — see privacy.go.
	Prompt string `json:"prompt,omitempty"`

	// ToolName / ToolInput / ToolResponse populate PostToolUse. ToolInput
	// is the raw JSON CC was about to invoke the tool with; ToolResponse
	// is the result. We hash ToolInput and never log it verbatim by default.
	ToolName     string          `json:"tool_name,omitempty"`
	ToolInput    json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse json.RawMessage `json:"tool_response,omitempty"`

	// Subagent identifies the subagent on SubagentStop events.
	Subagent string `json:"subagent,omitempty"`

	// Extras retains every other top-level key for future use without
	// requiring a code change here. Captured by ParsePayload.
	Extras map[string]json.RawMessage `json:"-"`
}

// ParsePayload decodes the CC hook JSON from r into a HookPayload. It
// preserves unknown top-level fields in Extras so we can surface them in
// metadata when useful. A trailing newline is tolerated.
func ParsePayload(r io.Reader) (*HookPayload, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var extras map[string]json.RawMessage
	if err := json.Unmarshal(raw, &extras); err != nil {
		return nil, err
	}
	var p HookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	// Strip out the known fields so Extras carries only the unknown
	// remainder.
	for _, k := range knownKeys {
		delete(extras, k)
	}
	if len(extras) > 0 {
		p.Extras = extras
	}
	return &p, nil
}

var knownKeys = []string{
	"session_id",
	"transcript_path",
	"cwd",
	"hook_event_name",
	"model",
	"permission_mode",
	"prompt",
	"tool_name",
	"tool_input",
	"tool_response",
	"subagent",
}
