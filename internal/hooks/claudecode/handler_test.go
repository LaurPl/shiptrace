package claudecode

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
)

type harness struct {
	t         *testing.T
	home      string
	eventsDir string
	writer    *eventlog.Writer
	sessions  *SessionMap
	handler   *Handler
	now       time.Time
	warnings  []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	home := t.TempDir()
	eventsDir := filepath.Join(home, "events")
	if err := os.MkdirAll(eventsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	w, err := eventlog.New(eventsDir)
	if err != nil {
		t.Fatalf("eventlog.New: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	m, err := NewSessionMap(home)
	if err != nil {
		t.Fatalf("NewSessionMap: %v", err)
	}

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	counter := 0
	h := New(w, m, home)
	h.Now = func() time.Time { return now }
	h.IDGen = func() string {
		counter++
		return fakeID(counter)
	}
	hn := &harness{t: t, home: home, eventsDir: eventsDir, writer: w, sessions: m, handler: h, now: now}
	// Capture warnings instead of letting them hit stderr, so tests can assert
	// on the self-heal nag.
	h.Warn = func(msg string) { hn.warnings = append(hn.warnings, msg) }
	return hn
}

func fakeID(n int) string {
	suffix := strings.Repeat("a", 12-len(intStr(n))) + intStr(n)
	return "shp_" + suffix
}

func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

func (h *harness) readEvents() []events.Event {
	h.t.Helper()
	matches, _ := filepath.Glob(filepath.Join(h.eventsDir, "*.jsonl"))
	if len(matches) == 0 {
		return nil
	}
	var out []events.Event
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			h.t.Fatalf("open: %v", err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var e events.Event
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				h.t.Fatalf("parse: %v", err)
			}
			out = append(out, e)
		}
		_ = f.Close()
	}
	return out
}

func TestHandleSessionStartEmitsEventAndMapsID(t *testing.T) {
	h := newHarness(t)

	err := h.handler.HandleSessionStart(&HookPayload{
		SessionID: "cc-uuid-1",
		Cwd:       "/Users/x/work/shiptrace",
		Model:     "claude-opus-4-7",
	})
	if err != nil {
		t.Fatalf("HandleSessionStart: %v", err)
	}

	mapped, _ := h.sessions.Get("cc-uuid-1")
	if mapped == "" {
		t.Fatalf("session map not populated")
	}

	ev := h.readEvents()
	if len(ev) != 1 || ev[0].EventType != events.SessionStart {
		t.Fatalf("expected 1 session_start event, got %+v", ev)
	}
	if ev[0].SessionID != mapped {
		t.Errorf("session id mismatch: event=%s map=%s", ev[0].SessionID, mapped)
	}
	if ev[0].Provider != Provider {
		t.Errorf("provider: %q", ev[0].Provider)
	}
	if !strings.Contains(ev[0].Label, "shiptrace") {
		t.Errorf("expected cwd basename in label, got %q", ev[0].Label)
	}
	if ev[0].Metadata["provider_session_id"] != "cc-uuid-1" {
		t.Errorf("provider_session_id missing in metadata: %+v", ev[0].Metadata)
	}
}

func TestHandleSessionStartRejectsMissingID(t *testing.T) {
	h := newHarness(t)
	if err := h.handler.HandleSessionStart(&HookPayload{}); err == nil {
		t.Fatalf("expected error for missing session_id")
	}
}

func TestHandlePromptHashesAndDetectsPivot(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-uuid-2", Cwd: "/x"})

	prompt := "Actually, scrap that and use Postgres"
	if err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-uuid-2", Prompt: prompt}); err != nil {
		t.Fatalf("HandlePrompt: %v", err)
	}
	ev := h.readEvents()
	if len(ev) != 3 {
		t.Fatalf("expected 3 events (session_start, prompt, replan_signal), got %d: %+v", len(ev), ev)
	}
	promptEv := ev[1]
	if promptEv.EventType != events.Prompt {
		t.Errorf("event[1] type: %q", promptEv.EventType)
	}
	if promptEv.Metadata["prompt_length"].(float64) != float64(len(prompt)) {
		t.Errorf("prompt_length: %v", promptEv.Metadata["prompt_length"])
	}
	if hash, _ := promptEv.Metadata["prompt_hash"].(string); !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("prompt_hash: %v", hash)
	}
	if _, leaked := promptEv.Metadata["prompt_text"]; leaked {
		t.Errorf("prompt_text leaked despite default privacy")
	}
	replanEv := ev[2]
	if replanEv.EventType != events.ReplanSignal {
		t.Errorf("event[2] type: %q", replanEv.EventType)
	}
	if replanEv.Metadata["kind"] != "pivot_phrase" {
		t.Errorf("replan kind: %v", replanEv.Metadata["kind"])
	}
}

func TestHandlePromptOptInVerbatim(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-uuid-3", Cwd: "/x"})

	t.Setenv(EnvLogPromptText, "1")
	if err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-uuid-3", Prompt: "verbatim"}); err != nil {
		t.Fatalf("HandlePrompt: %v", err)
	}
	ev := h.readEvents()
	promptEv := ev[1]
	if promptEv.Metadata["prompt_text"] != "verbatim" {
		t.Errorf("opt-in failed: %+v", promptEv.Metadata)
	}
}

// TestHandlePromptSelfHealsWithoutMapping locks in the recovery behavior: a
// prompt for a session we never saw a SessionStart for must NOT error and must
// NOT be dropped. resolveSession adopts the session — minting an id, persisting
// the mapping, and backfilling a synthetic session_start — so the prompt still
// lands. This is the regression guard for the "no shp_ id mapping" failure.
func TestHandlePromptSelfHealsWithoutMapping(t *testing.T) {
	h := newHarness(t)
	if err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-orphan", Cwd: "/x", Prompt: "hi"}); err != nil {
		t.Fatalf("HandlePrompt should self-heal, got error: %v", err)
	}

	// The session is now mapped...
	mapped, _ := h.sessions.Get("cc-orphan")
	if mapped == "" {
		t.Fatalf("session map not populated by self-heal")
	}

	// ...and the log carries a synthetic session_start followed by the prompt.
	ev := h.readEvents()
	if len(ev) != 2 {
		t.Fatalf("expected 2 events (synthetic session_start, prompt), got %d: %+v", len(ev), ev)
	}
	if ev[0].EventType != events.SessionStart {
		t.Errorf("event[0] type: %q want session_start", ev[0].EventType)
	}
	if ev[0].Metadata["synthetic_start"] != true {
		t.Errorf("backfilled start not marked synthetic: %+v", ev[0].Metadata)
	}
	if ev[1].EventType != events.Prompt {
		t.Errorf("event[1] type: %q want prompt", ev[1].EventType)
	}
	if ev[0].SessionID != mapped || ev[1].SessionID != mapped {
		t.Errorf("events not stamped with mapped id: start=%s prompt=%s map=%s", ev[0].SessionID, ev[1].SessionID, mapped)
	}

	// Self-heal recovers the data but must still fail loud once, so a
	// systemically broken SessionStart hook gets noticed (loud-fail doctrine).
	if len(h.warnings) != 1 {
		t.Fatalf("expected exactly 1 adoption warning, got %d: %v", len(h.warnings), h.warnings)
	}
	if !strings.Contains(h.warnings[0], "cc-orphan") || !strings.Contains(h.warnings[0], "SessionStart") {
		t.Errorf("adoption warning not descriptive: %q", h.warnings[0])
	}
}

// TestSelfHealOrderingSurvivesAppendFailure pins the Append-before-Set
// ordering: if the session_start Append fails during adoption, NO mapping may
// be persisted, so the next event retries adoption instead of resolving to a
// start-less session (which would be permanent in the append-only log).
func TestSelfHealOrderingSurvivesAppendFailure(t *testing.T) {
	h := newHarness(t)
	// Remove the events dir so the writer's OpenFile (and thus the session_start
	// Append) fails — the writer reopens lazily, so closing it wouldn't.
	if err := os.RemoveAll(h.eventsDir); err != nil {
		t.Fatalf("rm eventsDir: %v", err)
	}

	err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-fail", Cwd: "/x", Prompt: "hi"})
	if err == nil {
		t.Fatalf("expected error when session_start Append fails")
	}
	if mapped, _ := h.sessions.Get("cc-fail"); mapped != "" {
		t.Fatalf("mapping must NOT be persisted when the start Append failed, got %q", mapped)
	}
}

func TestHandleToolUseEmitsToolEvent(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-uuid-4", Cwd: "/x"})

	input := json.RawMessage(`{"file_path":"/x/y.go","old_string":"a","new_string":"b"}`)
	err := h.handler.HandleToolUse(&HookPayload{
		SessionID: "cc-uuid-4",
		ToolName:  "Edit",
		ToolInput: input,
	})
	if err != nil {
		t.Fatalf("HandleToolUse: %v", err)
	}
	ev := h.readEvents()
	toolEv := ev[1]
	if toolEv.EventType != events.ToolUse || toolEv.Tool != "Edit" {
		t.Errorf("tool event: %+v", toolEv)
	}
	if !strings.HasPrefix(toolEv.ToolInputHash, "sha256:") {
		t.Errorf("ToolInputHash: %q", toolEv.ToolInputHash)
	}
	if len(toolEv.FilesTouched) != 1 || toolEv.FilesTouched[0] != "/x/y.go" {
		t.Errorf("FilesTouched: %+v", toolEv.FilesTouched)
	}
	if _, leak := toolEv.Metadata["tool_input"]; leak {
		t.Errorf("tool_input leaked despite default privacy")
	}
}

// TestHandleToolUseExtractsAllFourPathFields pins the documented surface of
// files_touched extraction (see docs/privacy.md). If we ever shrink or grow
// this set, the privacy doc and this test must move together.
func TestHandleToolUseExtractsAllFourPathFields(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-paths", Cwd: "/x"})

	input := json.RawMessage(`{
		"file_path": "/x/a.go",
		"path":      "/x/b.go",
		"filename":  "/x/c.go",
		"files":     ["/x/d.go", "/x/e.go"]
	}`)
	if err := h.handler.HandleToolUse(&HookPayload{
		SessionID: "cc-paths",
		ToolName:  "MultiEdit",
		ToolInput: input,
	}); err != nil {
		t.Fatalf("HandleToolUse: %v", err)
	}
	toolEv := h.readEvents()[1]
	want := []string{"/x/a.go", "/x/b.go", "/x/c.go", "/x/d.go", "/x/e.go"}
	if len(toolEv.FilesTouched) != len(want) {
		t.Fatalf("FilesTouched count: got %d want %d (%v)", len(toolEv.FilesTouched), len(want), toolEv.FilesTouched)
	}
	for i, p := range want {
		if toolEv.FilesTouched[i] != p {
			t.Errorf("FilesTouched[%d] = %q, want %q", i, toolEv.FilesTouched[i], p)
		}
	}
}

// TestHandleToolUseDoesNotLogToolResponse confirms the "read but discard"
// contract in payload.go's HookPayload struct comment: tool_response goes
// nowhere — not the metadata map, not FilesTouched, not the hash field.
func TestHandleToolUseDoesNotLogToolResponse(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-resp", Cwd: "/x"})

	if err := h.handler.HandleToolUse(&HookPayload{
		SessionID:    "cc-resp",
		ToolName:     "Read",
		ToolInput:    json.RawMessage(`{"file_path":"/x/secret.txt"}`),
		ToolResponse: json.RawMessage(`{"contents":"SUPER SECRET CONTENT"}`),
	}); err != nil {
		t.Fatalf("HandleToolUse: %v", err)
	}
	toolEv := h.readEvents()[1]
	body, _ := json.Marshal(toolEv)
	if strings.Contains(string(body), "SUPER SECRET CONTENT") {
		t.Errorf("tool_response content leaked into event:\n%s", body)
	}
	if _, ok := toolEv.Metadata["tool_response"]; ok {
		t.Errorf("tool_response key present in metadata")
	}
}

func TestHandleToolUseTodoWriteEmitsReplanSignal(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-uuid-5", Cwd: "/x"})

	input := json.RawMessage(`{"todos":[
		{"status":"pending"},{"status":"in_progress"},{"status":"completed"}
	]}`)
	err := h.handler.HandleToolUse(&HookPayload{
		SessionID: "cc-uuid-5",
		ToolName:  "TodoWrite",
		ToolInput: input,
	})
	if err != nil {
		t.Fatalf("HandleToolUse: %v", err)
	}
	ev := h.readEvents()
	if len(ev) != 3 {
		t.Fatalf("expected 3 events (start, tool_use, replan_signal), got %d", len(ev))
	}
	rs := ev[2]
	if rs.EventType != events.ReplanSignal || rs.Metadata["kind"] != "todowrite" {
		t.Errorf("replan signal: %+v", rs)
	}
	if rs.Metadata["total"].(float64) != 3 {
		t.Errorf("total: %v", rs.Metadata["total"])
	}
}

func TestHandleSessionEndCleansUpSessionMap(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-uuid-6", Cwd: "/x"})

	if err := h.handler.HandleSessionEnd(&HookPayload{SessionID: "cc-uuid-6"}); err != nil {
		t.Fatalf("HandleSessionEnd: %v", err)
	}
	mapped, _ := h.sessions.Get("cc-uuid-6")
	if mapped != "" {
		t.Errorf("session map not cleaned: %q", mapped)
	}
	ev := h.readEvents()
	if ev[len(ev)-1].EventType != events.SessionStop {
		t.Errorf("last event should be session_stop")
	}
}

// TestHandleStopKeepsMappingAcrossTurns is the core regression guard for the
// turn-vs-session bug. Stop fires at the end of EVERY assistant turn; it must
// not emit a session_stop or delete the mapping, or the next prompt/tool_use
// in the same live session would be orphaned. We fire several Stops and assert
// the mapping survives, no session_stop is written, and a subsequent prompt
// still resolves to the original id without self-healing into a new one.
func TestHandleStopKeepsMappingAcrossTurns(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-live", Cwd: "/x"})
	original, _ := h.sessions.Get("cc-live")

	for i := 0; i < 3; i++ {
		if err := h.handler.HandleStop(&HookPayload{SessionID: "cc-live", Cwd: "/x"}); err != nil {
			t.Fatalf("HandleStop turn %d: %v", i, err)
		}
		if mapped, _ := h.sessions.Get("cc-live"); mapped != original {
			t.Fatalf("Stop turn %d changed/cleared mapping: %q (want %q)", i, mapped, original)
		}
	}

	if err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-live", Cwd: "/x", Prompt: "still here"}); err != nil {
		t.Fatalf("HandlePrompt after stops: %v", err)
	}

	ev := h.readEvents()
	for _, e := range ev {
		if e.EventType == events.SessionStop {
			t.Errorf("Stop must not emit session_stop, found one: %+v", e)
		}
		if e.EventType == events.SessionStart && e.Metadata["synthetic_start"] == true {
			t.Errorf("prompt should resolve the live mapping, not self-heal a new session")
		}
	}
	// A cleanly-mapped session must never emit the adoption nag.
	if len(h.warnings) != 0 {
		t.Errorf("no adoption warning expected on the mapped path, got: %v", h.warnings)
	}
}

// TestHandleSessionEndWithoutStartIsOrphan locks in the policy that a
// SessionEnd without a matching SessionStart (and therefore no
// cc-sessions/<uuid> mapping) drops the event quietly. This happens at install
// boundaries where CC sessions opened before `shiptrace init` ran fire
// SessionEnd on shutdown but never fired Start under the new hooks.
// Synthesizing a session_stop in that case produces a phantom "session" with
// no preceding start/prompt/tool_use and pollutes the dashboard.
func TestHandleSessionEndWithoutStartIsOrphan(t *testing.T) {
	h := newHarness(t)
	if err := h.handler.HandleSessionEnd(&HookPayload{SessionID: "cc-orphan"}); err != nil {
		t.Fatalf("HandleSessionEnd: %v", err)
	}
	ev := h.readEvents()
	if len(ev) != 0 {
		t.Fatalf("orphan SessionEnd must not emit events, got %+v", ev)
	}
}

func TestHandleSessionStartWritesProjectPointer(t *testing.T) {
	h := newHarness(t)
	cwd := t.TempDir() // a real existing dir so the pointer can resolve
	if err := h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-pp", Cwd: cwd}); err != nil {
		t.Fatalf("HandleSessionStart: %v", err)
	}

	pointerPath, err := projectPointerPath(h.home, cwd)
	if err != nil {
		t.Fatalf("pointerPath: %v", err)
	}
	if _, err := readActivePointer(pointerPath); err != nil {
		t.Fatalf("pointer not written: %v", err)
	}

	// SessionEnd cleans up.
	if err := h.handler.HandleSessionEnd(&HookPayload{SessionID: "cc-pp", Cwd: cwd}); err != nil {
		t.Fatalf("HandleSessionEnd: %v", err)
	}
	if _, statErr := readActivePointer(pointerPath); statErr == nil {
		t.Fatalf("pointer should be cleared after HandleSessionEnd")
	}
}

func TestHandlePromptTouchesProjectPointer(t *testing.T) {
	h := newHarness(t)
	cwd := t.TempDir()
	if err := h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-touch", Cwd: cwd}); err != nil {
		t.Fatalf("HandleSessionStart: %v", err)
	}
	// Advance the handler's clock so Touch produces a visible change.
	later := h.now.Add(10 * time.Minute)
	h.handler.Now = func() time.Time { return later }

	if err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-touch", Cwd: cwd, Prompt: "hi"}); err != nil {
		t.Fatalf("HandlePrompt: %v", err)
	}
	pointerPath, _ := projectPointerPath(h.home, cwd)
	ptr, err := readActivePointer(pointerPath)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if !ptr.LastActivity.Equal(later) {
		t.Errorf("LastActivity not advanced: got %v want %v", ptr.LastActivity, later)
	}
}
