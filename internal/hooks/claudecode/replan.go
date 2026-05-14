package claudecode

import (
	"encoding/json"
	"regexp"
	"strings"
)

// PivotPhraseRegex matches the cheap surface signals of mid-session replans.
// We deliberately keep this list short and conservative; the build-plan
// goal is "Best: regex pivot phrases", not NLP. "wait" is only matched
// when followed by punctuation, to avoid pulling in "wait for" / "wait
// until" / "wait while". False positives on the rest are preferable to
// false negatives at v0.1 — over-counting "actually" is information;
// missing "scrap that" is not.
var PivotPhraseRegex = regexp.MustCompile(
	`(?i)` +
		`\bactually\b|` +
		`\bwait[,!.?—-]|` +
		`\bhold on\b|` +
		`\blet'?s instead\b|` +
		`\bscrap that\b|` +
		`\bignore that\b|` +
		`\bnever mind\b|` +
		`\bundo that\b|` +
		`\bgo back\b|` +
		`\bon second thought\b`,
)

// DetectPivotPhrase returns the matched phrase (lowercased) if the prompt
// contains a known pivot signal, or "" if not.
func DetectPivotPhrase(prompt string) string {
	if prompt == "" {
		return ""
	}
	m := PivotPhraseRegex.FindString(prompt)
	if m == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(m))
}

// TodoStatusCounts summarizes a TodoWrite payload by counting how many items
// are in each status. Used by the ingester (day 4 will be the consumer) to
// detect status reversals across consecutive TodoWrite invocations — the
// "Better" signal from the build plan.
//
// The day-2 hook emits the raw counts plus the payload hash; day-4 reasoning
// happens in the ingester, not in the hot path.
type TodoStatusCounts struct {
	Pending    int `json:"pending"`
	InProgress int `json:"in_progress"`
	Completed  int `json:"completed"`
	Total      int `json:"total"`
}

type todoItem struct {
	Status string `json:"status"`
}

// SummarizeTodoWriteInput parses a TodoWrite tool_input payload and returns
// the per-status counts. Returns zero counts and nil error if the payload
// is missing or shaped differently than we expect — TodoWrite shape has
// shifted in the past and we don't want hook failures over it.
func SummarizeTodoWriteInput(toolInput json.RawMessage) (TodoStatusCounts, error) {
	var counts TodoStatusCounts
	if len(toolInput) == 0 {
		return counts, nil
	}
	// Two known shapes: {"todos":[...]} (current) and a bare [...] (older).
	// Try the wrapped shape first, fall back to bare.
	var wrapped struct {
		Todos []todoItem `json:"todos"`
	}
	if err := json.Unmarshal(toolInput, &wrapped); err == nil && wrapped.Todos != nil {
		return countStatuses(wrapped.Todos), nil
	}
	var bare []todoItem
	if err := json.Unmarshal(toolInput, &bare); err == nil {
		return countStatuses(bare), nil
	}
	return counts, nil
}

func countStatuses(items []todoItem) TodoStatusCounts {
	var c TodoStatusCounts
	for _, t := range items {
		c.Total++
		switch strings.ToLower(t.Status) {
		case "pending":
			c.Pending++
		case "in_progress":
			c.InProgress++
		case "completed":
			c.Completed++
		}
	}
	return c
}
