package claudecode

import (
	"encoding/json"
	"testing"
)

func TestDetectPivotPhrase(t *testing.T) {
	hits := []struct {
		prompt string
		want   string
	}{
		{"actually let's use Postgres", "actually"},
		{"Actually, scratch that", "actually"},
		{"wait, can you redo the schema", "wait,"},
		{"hold on — that test is wrong", "hold on"},
		{"let's instead try a different lib", "let's instead"},
		{"scrap that approach", "scrap that"},
		{"ignore that file", "ignore that"},
		{"never mind, undo it", "never mind"},
		{"undo that change you just made", "undo that"},
		{"go back to the previous design", "go back"},
		{"on second thought, do X", "on second thought"},
	}
	for _, c := range hits {
		got := DetectPivotPhrase(c.prompt)
		if got != c.want {
			t.Errorf("DetectPivotPhrase(%q) = %q, want %q", c.prompt, got, c.want)
		}
	}
}

func TestDetectPivotPhraseMisses(t *testing.T) {
	clean := []string{
		"",
		"add a new test for the parser",
		"the actual answer is 42", // "actual" without word-boundary should miss
		"wait for it to load",     // "wait for" without comma/space-end should miss
		"undoing the migration manually",
	}
	for _, c := range clean {
		if got := DetectPivotPhrase(c); got != "" {
			t.Errorf("DetectPivotPhrase(%q) = %q, want empty", c, got)
		}
	}
}

func TestSummarizeTodoWriteWrappedShape(t *testing.T) {
	raw := json.RawMessage(`{"todos":[
		{"status":"pending"},
		{"status":"in_progress"},
		{"status":"completed"},
		{"status":"completed"}
	]}`)
	c, err := SummarizeTodoWriteInput(raw)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if c.Pending != 1 || c.InProgress != 1 || c.Completed != 2 || c.Total != 4 {
		t.Errorf("counts: %+v", c)
	}
}

func TestSummarizeTodoWriteBareShape(t *testing.T) {
	raw := json.RawMessage(`[{"status":"pending"},{"status":"completed"}]`)
	c, _ := SummarizeTodoWriteInput(raw)
	if c.Pending != 1 || c.Completed != 1 || c.Total != 2 {
		t.Errorf("counts: %+v", c)
	}
}

func TestSummarizeTodoWriteUnknownShapeIsTolerated(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`{"foo":"bar"}`),
		json.RawMessage(`null`),
		nil,
	} {
		c, err := SummarizeTodoWriteInput(raw)
		if err != nil {
			t.Errorf("unexpected err for %s: %v", raw, err)
		}
		if c.Total != 0 {
			t.Errorf("expected zero counts for %s, got %+v", raw, c)
		}
	}
}
