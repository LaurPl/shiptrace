package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/ccsettings"
	"github.com/LaurPl/shiptrace/internal/paths"
)

// HookLatencyBudgetMs is the p99 budget per CC hook invocation. Doctor
// reports red when measured p99 exceeds this.
const HookLatencyBudgetMs = 30

func newDoctorCommand(out io.Writer) *cobra.Command {
	var samples int
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Verify hooks, watchers, and per-hook latency",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), out, samples)
		},
	}
	cmd.Flags().IntVar(&samples, "samples", 10, "How many synthetic hook invocations to time")
	return cmd
}

type checkResult struct {
	name   string
	status string // ✓ | ⚠ | ✗
	detail string
}

func runDoctor(ctx context.Context, out io.Writer, samples int) error {
	results := []checkResult{
		checkHome(),
		checkHookBinary(),
		checkSettings(),
		checkLatency(ctx, samples),
	}

	fmt.Fprintln(out)
	for _, r := range results {
		fmt.Fprintf(out, "  %s  %s — %s\n", r.status, r.name, r.detail)
	}
	fmt.Fprintln(out)

	for _, r := range results {
		if r.status == "✗" {
			return fmt.Errorf("shiptrace doctor: one or more checks failed")
		}
	}
	return nil
}

func checkHome() checkResult {
	home, err := paths.Home()
	if err != nil {
		return checkResult{"shiptrace home", "✗", err.Error()}
	}
	eventsDir, err := paths.EventsDir()
	if err != nil {
		return checkResult{"events dir", "✗", err.Error()}
	}
	// Touch a probe file to confirm we can write.
	probe := filepath.Join(eventsDir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return checkResult{"shiptrace home", "✗", fmt.Sprintf("not writable: %v", err)}
	}
	_ = os.Remove(probe)
	return checkResult{"shiptrace home", "✓", home}
}

func checkHookBinary() checkResult {
	if path, err := exec.LookPath(HookBinaryName); err == nil {
		return checkResult{"hook binary", "✓", path}
	}
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), HookBinaryName)
		if _, err := os.Stat(candidate); err == nil {
			return checkResult{"hook binary", "⚠", fmt.Sprintf("found next to shiptrace at %s (not on PATH — add it or use `shiptrace init --hook-bin`)", candidate)}
		}
	}
	return checkResult{"hook binary", "✗", fmt.Sprintf("%s not found on PATH (build with `go install ./cmd/%s`)", HookBinaryName, HookBinaryName)}
}

func checkSettings() checkResult {
	path, err := ccsettings.DefaultSettingsPath()
	if err != nil {
		return checkResult{"settings.json", "✗", err.Error()}
	}
	settings, err := ccsettings.Read(path)
	if err != nil {
		return checkResult{"settings.json", "✗", err.Error()}
	}
	if _, err := os.Stat(path); err != nil {
		return checkResult{"settings.json", "⚠", fmt.Sprintf("not present at %s — run `shiptrace init`", path)}
	}
	present, missing := settings.HasShiptraceHooks()
	if len(missing) > 0 {
		var names []string
		for _, m := range missing {
			names = append(names, string(m))
		}
		return checkResult{"settings.json hooks", "✗", fmt.Sprintf("missing %d hook(s): %s — run `shiptrace init`", len(missing), strings.Join(names, ", "))}
	}
	var names []string
	for _, p := range present {
		names = append(names, string(p))
	}
	return checkResult{"settings.json hooks", "✓", fmt.Sprintf("all 5 installed (%s)", strings.Join(names, ", "))}
}

// checkLatency invokes shiptrace-cc-hook several times against a temp
// SHIPTRACE_HOME so it never pollutes user data. Reports p50 and p99
// across the samples.
func checkLatency(ctx context.Context, samples int) checkResult {
	bin, err := exec.LookPath(HookBinaryName)
	if err != nil {
		exe, eerr := os.Executable()
		if eerr == nil {
			candidate := filepath.Join(filepath.Dir(exe), HookBinaryName)
			if _, serr := os.Stat(candidate); serr == nil {
				bin = candidate
			}
		}
	}
	if bin == "" {
		return checkResult{"hook latency", "⚠", "hook binary unavailable — skipped"}
	}
	if samples < 1 {
		samples = 10
	}

	tmp, err := os.MkdirTemp("", "shiptrace-doctor-")
	if err != nil {
		return checkResult{"hook latency", "✗", err.Error()}
	}
	defer os.RemoveAll(tmp)

	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		// Build the synthetic payload via json.Marshal so the path in `tmp`
		// — which on Windows is full of backslashes that JSON treats as
		// escapes — round-trips correctly.
		payload, err := json.Marshal(struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
		}{
			SessionID: fmt.Sprintf("doctor-%d", i),
			Cwd:       tmp,
		})
		if err != nil {
			return checkResult{"hook latency", "✗", fmt.Sprintf("marshal payload: %v", err)}
		}

		start := time.Now()
		cmd := exec.CommandContext(ctx, bin, "session-start")
		cmd.Env = append(os.Environ(), "SHIPTRACE_HOME="+tmp)
		cmd.Stdin = bytes.NewReader(payload)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return checkResult{"hook latency", "✗", fmt.Sprintf("invocation %d failed: %v (%s)", i, err, stderr.String())}
		}
		durations = append(durations, time.Since(start))
	}

	p50 := percentile(durations, 0.50)
	p99 := percentile(durations, 0.99)
	status := "✓"
	if p99 > time.Duration(HookLatencyBudgetMs)*time.Millisecond {
		status = "✗"
	}
	return checkResult{
		"hook latency",
		status,
		fmt.Sprintf("p50=%.1fms p99=%.1fms across %d invocations (budget %dms)", float64(p50.Microseconds())/1000, float64(p99.Microseconds())/1000, samples, HookLatencyBudgetMs),
	}
}

func percentile(in []time.Duration, p float64) time.Duration {
	if len(in) == 0 {
		return 0
	}
	cp := make([]time.Duration, len(in))
	copy(cp, in)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(float64(len(cp)-1) * p)
	return cp[idx]
}
