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
	return &harness{t: t, home: home, eventsDir: eventsDir, writer: w, sessions: m, handler: h, now: now}
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

func TestHandlePromptWithoutSessionMappingErrors(t *testing.T) {
	h := newHarness(t)
	err := h.handler.HandlePrompt(&HookPayload{SessionID: "cc-orphan", Prompt: "hi"})
	if err == nil {
		t.Fatalf("expected error for missing mapping")
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

func TestHandleStopCleansUpSessionMap(t *testing.T) {
	h := newHarness(t)
	_ = h.handler.HandleSessionStart(&HookPayload{SessionID: "cc-uuid-6", Cwd: "/x"})

	if err := h.handler.HandleStop(&HookPayload{SessionID: "cc-uuid-6"}); err != nil {
		t.Fatalf("HandleStop: %v", err)
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

// TestHandleStopWithoutStartIsOrphan locks in the policy that a Stop hook
// without a matching SessionStart (and therefore no cc-sessions/<uuid>
// mapping) drops the event quietly. This happens at install boundaries
// where CC sessions opened before `shiptrace init` ran fire Stop on
// shutdown but never fired Start under the new hooks. Synthesizing a
// session_stop in that case produces a phantom "session" with no preceding
// start/prompt/tool_use and pollutes the dashboard.
func TestHandleStopWithoutStartIsOrphan(t *testing.T) {
	h := newHarness(t)
	if err := h.handler.HandleStop(&HookPayload{SessionID: "cc-orphan"}); err != nil {
		t.Fatalf("HandleStop: %v", err)
	}
	ev := h.readEvents()
	if len(ev) != 0 {
		t.Fatalf("orphan Stop must not emit events, got %+v", ev)
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

	// Stop cleans up.
	if err := h.handler.HandleStop(&HookPayload{SessionID: "cc-pp", Cwd: cwd}); err != nil {
		t.Fatalf("HandleStop: %v", err)
	}
	if _, statErr := readActivePointer(pointerPath); statErr == nil {
		t.Fatalf("pointer should be cleared after HandleStop")
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
