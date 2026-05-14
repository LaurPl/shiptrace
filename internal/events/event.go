// Package events defines the canonical schema for every event flowing through
// shiptrace, regardless of provider. Recorders marshal Events; the eventlog
// appends them; the ingester materializes a subset into SQLite. The schema is
// the contract between every other package — keep it stable.
package events

import "time"

// SchemaVersion is the value emitted on every event. Bump this when a
// breaking change to the Event shape lands, and add a migration in the
// ingester that knows how to upgrade older lines.
const SchemaVersion = "1"

// EventType enumerates the canonical event kinds. Add new kinds at the end of
// the list to keep ordering stable for grep-archaeology.
type EventType string

const (
	SessionStart EventType = "session_start"
	SessionStop  EventType = "session_stop"
	Prompt       EventType = "prompt"
	ToolUse      EventType = "tool_use"
	ReplanSignal EventType = "replan_signal"
	Ship         EventType = "ship"
)

// TokenCount carries per-event token usage when the provider supplies it.
type TokenCount struct {
	In  int `json:"in"`
	Out int `json:"out"`
}

// Event is the canonical record. Optional fields use omitempty so JSONL lines
// stay terse; the schema_version is always emitted.
type Event struct {
	SchemaVersion string         `json:"schema_version"`
	EventType     EventType      `json:"event_type"`
	Ts            time.Time      `json:"ts"`
	SessionID     string         `json:"session_id,omitempty"`
	Provider      string         `json:"provider,omitempty"`
	Project       string         `json:"project,omitempty"`
	Agent         string         `json:"agent,omitempty"`
	Skill         string         `json:"skill,omitempty"`
	Model         string         `json:"model,omitempty"`
	Tool          string         `json:"tool,omitempty"`
	ToolInputHash string         `json:"tool_input_hash,omitempty"`
	FilesTouched  []string       `json:"files_touched,omitempty"`
	Tokens        *TokenCount    `json:"tokens,omitempty"`
	Label         string         `json:"label,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// WithDefaults returns a copy of e with SchemaVersion and Ts populated if
// they were zero. Call this before Append so recorders don't have to remember.
func (e Event) WithDefaults() Event {
	if e.SchemaVersion == "" {
		e.SchemaVersion = SchemaVersion
	}
	if e.Ts.IsZero() {
		e.Ts = time.Now().UTC()
	}
	return e
}
