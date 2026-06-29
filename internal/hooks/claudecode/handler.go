package claudecode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/project"
	"github.com/LaurPl/shiptrace/internal/session"
)

// Provider is the value stamped on Provider for every event emitted by this
// recorder. Kept as a constant so cross-package consumers can match on it.
const Provider = "claude-code"

// Handler is the entry point used by cmd/shiptrace-cc-hook. It owns the
// eventlog writer and the session-id map; one Handler is constructed per
// hook invocation (which is fine because both are O(1) to create).
//
// Home is the resolved SHIPTRACE_HOME directory; the handler writes the
// per-project pointer underneath it on SessionStart so the git post-commit
// adapter can find which shp_ session a commit belongs to.
type Handler struct {
	Writer   *eventlog.Writer
	Sessions *SessionMap
	Home     string
	IDGen    func() string          // override for tests
	Now      func() time.Time       // override for tests
	Hostname func() (string, error) // override for tests
	// Warn receives non-fatal recorder warnings (e.g. a self-heal adoption).
	// It defaults to stderr; tests override it. Kept separate from returned
	// errors because the event WAS recorded — we want to nag the user that
	// SessionStart isn't firing, not fail the hook and drop the event.
	Warn func(string)
}

// New constructs a Handler with sensible production defaults.
func New(w *eventlog.Writer, m *SessionMap, home string) *Handler {
	return &Handler{
		Writer:   w,
		Sessions: m,
		Home:     home,
		IDGen:    events.NewSessionID,
		Now:      func() time.Time { return time.Now().UTC() },
		Hostname: func() (string, error) { return "", nil },
		Warn:     func(m string) { fmt.Fprintln(os.Stderr, "shiptrace-cc-hook:", m) },
	}
}

// HandleSessionStart materializes a session_start event for a CC session.
// It mints a shp_ id, emits the event, and persists the mapping ccID → shpID.
func (h *Handler) HandleSessionStart(p *HookPayload) error {
	if p.SessionID == "" {
		return errors.New("claudecode: SessionStart payload missing session_id")
	}
	_, err := h.adoptSession(p, false)
	return err
}

// adoptSession mints a shp_ id for p.SessionID, emits the session_start event,
// and only THEN persists the cc-uuid → shp_ mapping. Shared by the real
// SessionStart path (synthetic=false) and resolveSession's self-heal path
// (synthetic=true) so the two can't drift.
//
// Append-before-Set is deliberate and load-bearing: the JSONL log is the
// append-only source of truth and can never be rewritten, while the mapping
// file is a rebuildable cache. If we persisted the mapping first and the
// session_start Append then failed, the next event would resolve via the Get
// fast path and emit prompt/tool_use lines for a session whose start never
// reached the log — a permanent, invisible start-less session. Writing the
// start first means a failed Append leaves no mapping, so the next event
// simply retries adoption. The only downside is the reverse failure (Append
// ok, Set fails) yielding a duplicate session_start on retry — a visible,
// recoverable fault, strictly preferable to silent loss.
func (h *Handler) adoptSession(p *HookPayload, synthetic bool) (string, error) {
	shpID := h.IDGen()
	if err := h.emitSessionStart(p, shpID, synthetic); err != nil {
		return "", err
	}
	if err := h.Sessions.Set(p.SessionID, shpID); err != nil {
		return "", err
	}
	return shpID, nil
}

// emitSessionStart writes the session_start event and the per-project pointer
// for shpID. The synthetic flag marks starts that resolveSession backfilled
// because no real SessionStart hook was seen (see resolveSession), so
// downstream consumers can tell an adopted session apart from a cleanly-
// started one.
func (h *Handler) emitSessionStart(p *HookPayload, shpID string, synthetic bool) error {
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
	if synthetic {
		meta["synthetic_start"] = true
	}

	if err := h.Writer.Append(events.Event{
		EventType: events.SessionStart,
		Ts:        now,
		SessionID: shpID,
		Provider:  Provider,
		Project:   projectFromCwd(p.Cwd),
		Model:     p.Model,
		Label:     label,
		Metadata:  meta,
	}); err != nil {
		return err
	}

	// Write the per-project pointer so `shiptrace ship` and the git
	// post-commit adapter can attribute work without a flag.
	return h.writeProjectPointer(p.Cwd, shpID, label, now)
}

// writeProjectPointer is best-effort: failure is logged into the event
// metadata path but does NOT fail the hook, since hook failures can wedge
// the CC session.
func (h *Handler) writeProjectPointer(cwd, shpID, label string, now time.Time) error {
	if h.Home == "" || cwd == "" {
		return nil
	}
	path, err := session.ProjectPointerPath(h.Home, cwd)
	if err != nil {
		return nil
	}
	return session.WriteActive(path, session.ActivePointer{
		SessionID:    shpID,
		Label:        label,
		StartedAt:    now,
		LastActivity: now,
	})
}

// touchProjectPointer bumps LastActivity on the pointer for cwd, if one
// exists. Best-effort — never fails the hook.
func (h *Handler) touchProjectPointer(cwd string) {
	if h.Home == "" || cwd == "" {
		return
	}
	path, err := session.ProjectPointerPath(h.Home, cwd)
	if err != nil {
		return
	}
	_ = session.Touch(path, h.Now())
}

// clearProjectPointer removes the pointer for cwd; best-effort.
func (h *Handler) clearProjectPointer(cwd string) {
	if h.Home == "" || cwd == "" {
		return
	}
	path, err := session.ProjectPointerPath(h.Home, cwd)
	if err != nil {
		return
	}
	_ = session.ClearActive(path)
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
		if err := h.Writer.Append(events.Event{
			EventType: events.ReplanSignal,
			Ts:        now,
			SessionID: shpID,
			Provider:  Provider,
			Metadata: map[string]any{
				"kind":   "pivot_phrase",
				"phrase": phrase,
				"weight": 1.0,
			},
		}); err != nil {
			return err
		}
	}
	h.touchProjectPointer(p.Cwd)
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
		if err := h.Writer.Append(events.Event{
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
		}); err != nil {
			return err
		}
	}
	h.touchProjectPointer(p.Cwd)
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
	if err := h.Writer.Append(events.Event{
		EventType: events.ToolUse,
		Ts:        now,
		SessionID: shpID,
		Provider:  Provider,
		Tool:      "SubagentStop",
		Metadata: map[string]any{
			"subagent": p.Subagent,
		},
	}); err != nil {
		return err
	}
	h.touchProjectPointer(p.Cwd)
	return nil
}

// HandleStop fires at the END OF EVERY ASSISTANT TURN — not at session end.
// Claude Code emits Stop each time the main agent finishes responding, many
// times over a session's life. It must therefore be side-effect-light: it
// must NOT emit a session_stop and must NOT delete the session mapping.
// Doing either (as an earlier version did) tears the session down mid-flight
// and orphans every subsequent prompt/tool_use — the next resolveSession
// finds no mapping and the hook fails with "no shp_ id mapping". Real
// teardown lives in HandleSessionEnd, wired to CC's SessionEnd hook. Here we
// only keep the per-project pointer warm so git attribution doesn't go stale
// between turns. Best-effort; a missing pointer or cwd is a no-op.
func (h *Handler) HandleStop(p *HookPayload) error {
	h.touchProjectPointer(p.Cwd)
	return nil
}

// HandleSessionEnd emits the session_stop event and cleans up the
// cc-session-id mapping file. It is wired to Claude Code's SessionEnd hook,
// which fires once when a session actually ends (clear, logout, exit) — the
// correct boundary for "the session is over", unlike Stop.
//
// If the mapping is missing, we treat the end as orphaned and decline to emit
// any event. Orphaned ends happen at install boundaries: CC sessions already
// open when `shiptrace init` runs never fire SessionStart with the new hooks
// (CC reads settings.json at session-open time), and if no prompt/tool_use
// self-healed the session in between, there's no shp_ id to attach to.
// Synthesizing one would write a phantom session_stop with no preceding
// start — the ingester then materializes a "session" row at stop_ts with
// all-zero counts, which is pure noise on the dashboard. The
// path-of-least-surprise is to drop the event quietly and still clean up the
// per-cwd pointer and mapping.
func (h *Handler) HandleSessionEnd(p *HookPayload) error {
	if p.SessionID == "" {
		return errors.New("claudecode: SessionEnd payload missing session_id")
	}
	shpID, err := h.Sessions.Get(p.SessionID)
	if err != nil {
		return err
	}
	if shpID == "" {
		// Orphaned end — clean up best-effort and return.
		h.clearProjectPointer(p.Cwd)
		return h.Sessions.Delete(p.SessionID)
	}
	if err := h.Writer.Append(events.Event{
		EventType: events.SessionStop,
		Ts:        h.Now(),
		SessionID: shpID,
		Provider:  Provider,
		Metadata: map[string]any{
			"provider_session_id": p.SessionID,
		},
	}); err != nil {
		return err
	}
	h.clearProjectPointer(p.Cwd)
	return h.Sessions.Delete(p.SessionID)
}

// resolveSession looks up the shp_ id for the CC session_id, adopting the
// session on the fly if no mapping exists yet.
//
// A missing mapping is NOT an error: it happens whenever an event arrives for
// a session whose SessionStart we never saw — the session was already open
// when shiptrace was installed, or it ended and was cleaned up and is now
// being resumed. Rather than drop the event (silent data loss — exactly the
// failure mode the recorder is meant to avoid), we adopt the session: mint a
// shp_ id, persist the mapping, and backfill a synthetic session_start so the
// ingester has a real row to hang this prompt/tool_use on. Subsequent events
// in the same session then resolve straight through the Get fast path.
func (h *Handler) resolveSession(p *HookPayload) (string, error) {
	if p.SessionID == "" {
		return "", errors.New("claudecode: payload missing session_id")
	}
	shpID, err := h.Sessions.Get(p.SessionID)
	if err != nil {
		return "", err
	}
	if shpID != "" {
		return shpID, nil
	}
	// Self-heal: adopt the unknown session so the event isn't dropped...
	shpID, err = h.adoptSession(p, true)
	if err != nil {
		return "", err
	}
	// ...but still fail loud: a missing mapping means SessionStart didn't fire
	// for this session, and if that's systemic (a broken hook) the user needs
	// to know. The warning is non-fatal (the event was recorded) and fires
	// once per session — adoption only runs while the mapping is absent.
	if h.Warn != nil {
		h.Warn(fmt.Sprintf("adopted cc session %q with no prior SessionStart; recorded a synthetic session_start. If SessionStart should be firing, restart the CC session to restore full telemetry.", p.SessionID))
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

// projectFromCwd resolves the canonical project name. Worktree paths
// (CC's .claude/worktrees and git's `git worktree add` pointers) fold
// into the parent project so the dashboard doesn't surface every
// worktree as its own one-off project. See internal/project.Normalize.
func projectFromCwd(cwd string) string {
	return project.Normalize(cwd)
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
