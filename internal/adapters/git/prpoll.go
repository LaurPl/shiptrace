package git

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PRPollStateFileName is the file under SHIPTRACE_HOME where we remember
// which merged PR URLs we've already emitted ship events for.
const PRPollStateFileName = ".pr-merge-state.json"

// PRPollState is the on-disk cache of seen PR URLs. Keys are PR URLs
// because the gh CLI gives them as stable identifiers; the timestamp
// value is purely informational.
type PRPollState struct {
	Seen map[string]time.Time `json:"seen"`
}

// LoadPRPollState reads the state file, returning an empty state when no
// file exists yet.
func LoadPRPollState(path string) (*PRPollState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &PRPollState{Seen: map[string]time.Time{}}, nil
		}
		return nil, fmt.Errorf("git pr-poll: read state: %w", err)
	}
	var s PRPollState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("git pr-poll: parse state: %w", err)
	}
	if s.Seen == nil {
		s.Seen = map[string]time.Time{}
	}
	return &s, nil
}

// SavePRPollState writes s atomically.
func SavePRPollState(path string, s *PRPollState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// MergedPR is the slice of `gh pr list` output we materialize into ship
// events. Other fields are ignored; we lean on `gh` for parsing rather
// than reimplementing.
type MergedPR struct {
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Number      int       `json:"number"`
	MergedAt    time.Time `json:"mergedAt"`
	BaseRefName string    `json:"baseRefName,omitempty"`
}

// FetchMergedPRs invokes `gh pr list --state merged --json url,title,number,mergedAt,baseRefName --limit N`
// from inside repoDir and parses the JSON output. Returns an error if gh
// is missing or the call fails — callers should treat that as "skip this
// poll, try again later" rather than fatal.
func FetchMergedPRs(repoDir string, limit int) ([]MergedPR, error) {
	if limit <= 0 {
		limit = 50
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("git pr-poll: gh CLI not found on PATH; install https://cli.github.com")
	}
	cmd := exec.Command("gh", "pr", "list",
		"--state", "merged",
		"--json", "url,title,number,mergedAt,baseRefName",
		"--limit", fmt.Sprintf("%d", limit),
	)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(ee.Stderr))
		}
		return nil, fmt.Errorf("git pr-poll: gh failed: %w (%s)", err, stderr)
	}
	var prs []MergedPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("git pr-poll: parse gh output: %w", err)
	}
	return prs, nil
}

// FilterNew returns the subset of prs that aren't already in state.Seen.
// Caller is expected to mark them as seen after emitting events.
func FilterNew(state *PRPollState, prs []MergedPR) []MergedPR {
	if state == nil {
		return prs
	}
	var out []MergedPR
	for _, p := range prs {
		if _, seen := state.Seen[p.URL]; !seen {
			out = append(out, p)
		}
	}
	return out
}

// MarkSeen records each PR's URL as already-emitted with the merge
// timestamp.
func MarkSeen(state *PRPollState, prs []MergedPR) {
	if state.Seen == nil {
		state.Seen = map[string]time.Time{}
	}
	for _, p := range prs {
		state.Seen[p.URL] = p.MergedAt
	}
}
