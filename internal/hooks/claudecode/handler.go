package claudecode

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
)

// Provider is the value stamped on Provider for every event emitted by this
// recorder. Kept as a constant so cross-package consumers can match on it.
const Provider = "claude-code"

// Handler is the entry point used by cmd/shiptrace-cc-hook. It owns the
// eventlog writer and the session-id map; one Handler is constructed per
// hook invocation (which is fine because both are O(1) to create).
type Handler struct {
	Writer   *eventlog.Writer
	Sessions *SessionMap
	IDGen    func() string          // override for tests
	Now      func() time.Time       // override for tests
	Hostname func() (string, error) // override for tests
}

// New constructs a Handler with sensible production defaults.
func New(w *eventlog.Writer, m *SessionMap) *Handler {
	return &Handler{
		Writer:   w,
		Sessions: m,
		IDGen:    events.NewSessionID,
		Now:      func() time.Time { return time.Now().UTC() },
		Hostname: func() (string, error) { return "", nil },
	}
}

// HandleSessionStart materializes a session_start event for a CC session.
// It generates a shp_ id, persists the mapping ccID → shpID, and emits
// the event with privacy-safe metadata.
func (h *Handler) HandleSessionStart(p *HookPayload) error {
	if p.SessionID == "" {
		return errors.New("claudecode: SessionStart payload missing session_id")
	}
	shpID := h.IDGen()
	if err := h.Sessions.Set(p.SessionID, shpID); err != nil {
		return err
	}

	now := h.Now()
	label := defaultLabel(now, p.Cwd)

	meta := map[string]any{
		"provider_session_id": p.SessionID,
		"cwd":                 p.Cwd,
	}
	if p.PermissionMode != "" {
		meta["permission_mode"] = p.PermissionMode
	}
	if p.TranscriptPath != "" {
		meta["transcript_path"] = p.TranscriptPath
	}

	return h.Writer.Append(events.Event{
		EventType: events.SessionStart,
		Ts:        now,
		SessionID: shpID,
		Provider:  Provider,
		Project:   projectFromCwd(p.Cwd),
		Model:     p.Model,
		Label:     label,
		Metadata:  meta,
	})
}

// HandlePrompt emits a prompt event with length + sha256 hash, and a
// replan_signal if a pivot phrase is detected. Verbatim prompt text is
// captured only when the user has explicitly opted in via
// SHIPTRACE_LOG_PROMPT_TEXT=1.
func (h *Handler) HandlePrompt(p *HookPayload) error {
	shpID, err := h.resolveSession(p)
	if err != nil {
		return err
	}
	now := h.Now()

	meta := map[string]any{
		"prompt_length": len(p.Prompt),
		"prompt_hash":   HashString(p.Prompt),
	}
	if LogPromptText() {
		meta["prompt_text"] = p.Prompt
	}
	if err := h.Writer.Append(events.Event{
		EventType: events.Prompt,
		Ts:        now,
		SessionID: shpID,
		Provider:  Provider,
		Metadata:  meta,
	}); err != nil {
		return err
	}

	if phrase := DetectPivotPhrase(p.Prompt); phrase != "" {
		return h.Writer.Append(events.Event{
			EventType: events.ReplanSignal,
			Ts:        now,
			SessionID: shpID,
			Provider:  Provider,
			Metadata: map[string]any{
				"kind":   "pivot_phrase",
				"phrase": phrase,
				"weight": 1.0,
			},
		})
	}
	return nil
}

// HandleToolUse emits a tool_use event with the tool name, tool_input hash,
// and (when present and disclosed by the tool) the files touched. TodoWrite
// payloads also generate a paired replan_signal carrying status counts so
// day 4 can detect status reversals offline.
func (h *Handler) HandleToolUse(p *HookPayload) error {
	if p.ToolName == "" {
		return errors.New("claudecode: PostToolUse payload missing tool_name")
	}
	shpID, err := h.resolveSession(p)
	if err != nil {
		return err
	}
	now := h.Now()

	meta := map[string]any{
		"tool_input_size": len(p.ToolInput),
	}
	if LogToolInputText() && len(p.ToolInput) > 0 {
		meta["tool_input"] = string(p.ToolInput)
	}

	if err := h.Writer.Append(events.Event{
		EventType:     events.ToolUse,
		Ts:            now,
		SessionID:     shpID,
		Provider:      Provider,
		Tool:          p.ToolName,
		ToolInputHash: HashBytes(p.ToolInput),
		FilesTouched:  extractFilesTouched(p),
		Metadata:      meta,
	}); err != nil {
		return err
	}

	// TodoWrite emits a paired replan_signal event so the ingester can
	// reason about status reversals across consecutive invocations.
	if p.ToolName == "TodoWrite" {
		counts, _ := SummarizeTodoWriteInput(p.ToolInput)
		return h.Writer.Append(events.Event{
			EventType: events.ReplanSignal,
			Ts:        now,
			SessionID: shpID,
			Provider:  Provider,
			Metadata: map[string]any{
				"kind":         "todowrite",
				"pending":      counts.Pending,
				"in_progress":  counts.InProgress,
				"completed":    counts.Completed,
				"total":        counts.Total,
				"payload_hash": HashBytes(p.ToolInput),
				"weight":       0.5, // raw TodoWrite is a weaker signal than a reversal
			},
		})
	}
	return nil
}

// HandleSubagentStop emits a synthetic tool_use event carrying the subagent
// name; we treat it like a tool boundary for now. Day 4 may promote it to
// a dedicated event_type if the analytics need it.
func (h *Handler) HandleSubagentStop(p *HookPayload) error {
	shpID, err := h.resolveSession(p)
	if err != nil {
		return err
	}
	now := h.Now()
	return h.Writer.Append(events.Event{
		EventType: events.ToolUse,
		Ts:        now,
		SessionID: shpID,
		Provider:  Provider,
		Tool:      "SubagentStop",
		Metadata: map[string]any{
			"subagent": p.Subagent,
		},
	})
}

// HandleStop emits the session_stop event and cleans up the cc-session-id
// mapping file. If the mapping is missing, we still emit an event with the
// CC session id stuffed into provider_session_id so the ingester can
// reconcile.
func (h *Handler) HandleStop(p *HookPayload) error {
	now := h.Now()
	shpID, err := h.Sessions.Get(p.SessionID)
	if err != nil {
		return err
	}
	if shpID == "" {
		// SessionStart never fired (or was lost). Synthesize an id so the
		// event isn't dropped; the ingester will backfill the sessions row.
		shpID = h.IDGen()
	}
	if err := h.Writer.Append(events.Event{
		EventType: events.SessionStop,
		Ts:        now,
		SessionID: shpID,
		Provider:  Provider,
		Metadata: map[string]any{
			"provider_session_id": p.SessionID,
		},
	}); err != nil {
		return err
	}
	return h.Sessions.Delete(p.SessionID)
}

// resolveSession looks up the shp_ id for the CC session_id, returning a
// helpful error when the mapping is missing (it should not be, after
// SessionStart fires).
func (h *Handler) resolveSession(p *HookPayload) (string, error) {
	if p.SessionID == "" {
		return "", errors.New("claudecode: payload missing session_id")
	}
	shpID, err := h.Sessions.Get(p.SessionID)
	if err != nil {
		return "", err
	}
	if shpID == "" {
		return "", fmt.Errorf("claudecode: no shp_ id mapping for cc session %q (SessionStart hook may have missed)", p.SessionID)
	}
	return shpID, nil
}

func defaultLabel(now time.Time, cwd string) string {
	base := filepath.Base(cwd)
	if base == "." || base == "/" || base == "" {
		base = "unknown"
	}
	return fmt.Sprintf("claude-code @ %s — %s", base, now.Format("2006-01-02 15:04"))
}

func projectFromCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Base(cwd)
}

// extractFilesTouched returns a best-effort list of files referenced by the
// tool_input. We only handle a handful of well-known shapes; day 3+ will
// formalize this and respect redact_paths from config.
func extractFilesTouched(p *HookPayload) []string {
	if len(p.ToolInput) == 0 {
		return nil
	}
	// Common shape: {"file_path": "..."} or {"path": "..."}. Best-effort
	// only — we never error on shapes we don't recognize.
	type fileFields struct {
		FilePath  string   `json:"file_path"`
		Path      string   `json:"path"`
		FilePath2 string   `json:"filename"`
		Files     []string `json:"files"`
	}
	var ff fileFields
	if err := jsonUnmarshalSafely(p.ToolInput, &ff); err != nil {
		return nil
	}
	var out []string
	for _, c := range []string{ff.FilePath, ff.Path, ff.FilePath2} {
		if c != "" {
			out = append(out, c)
		}
	}
	out = append(out, ff.Files...)
	if len(out) == 0 {
		return nil
	}
	return out
}
