package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/adapters/git"
	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/paths"
)

// PostCommitBinaryName is the binary the git adapter installer looks for.
const PostCommitBinaryName = "shiptrace-git-postcommit"

func newAdapterCommand(out, errOut io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapter",
		Short: "Install and manage ship adapters (git, pr-merge, …)",
	}
	cmd.AddCommand(
		newAdapterInstallCommand(out),
		newAdapterUninstallCommand(out),
		newAdapterStatusCommand(out),
		newAdapterPRPollCommand(out, errOut),
	)
	return cmd
}

func newAdapterInstallCommand(out io.Writer) *cobra.Command {
	var (
		repoFlag   string
		binaryFlag string
	)
	cmd := &cobra.Command{
		Use:   "install git",
		Short: "Install a ship adapter (currently: git post-commit only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "git" {
				return fmt.Errorf("unsupported adapter %q (only 'git' for now)", args[0])
			}
			repo, err := resolveRepoFlag(repoFlag)
			if err != nil {
				return err
			}
			binary, err := resolvePostCommitBinary(binaryFlag)
			if err != nil {
				return err
			}
			root, err := git.FindRepoRoot(repo)
			if err != nil {
				return err
			}
			changed, err := git.InstallPostCommit(root, binary)
			if err != nil {
				return err
			}
			if !changed {
				fmt.Fprintln(out, "✓ git post-commit already installed —", filepath.Join(root, ".git/hooks/post-commit"))
				return nil
			}
			fmt.Fprintln(out, "✓ git post-commit installed →", filepath.Join(root, ".git/hooks/post-commit"))
			fmt.Fprintln(out, "  hook binary:", binary)
			fmt.Fprintln(out, "  test it with: git commit --allow-empty -m \"smoke\"")
			return nil
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Target git repo (default: current working directory)")
	cmd.Flags().StringVar(&binaryFlag, "binary", "", fmt.Sprintf("Path to %s (default: resolve via $PATH)", PostCommitBinaryName))
	return cmd
}

func newAdapterUninstallCommand(out io.Writer) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "uninstall git",
		Short: "Remove the shiptrace block from .git/hooks/post-commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "git" {
				return fmt.Errorf("unsupported adapter %q", args[0])
			}
			repo, err := resolveRepoFlag(repoFlag)
			if err != nil {
				return err
			}
			root, err := git.FindRepoRoot(repo)
			if err != nil {
				return err
			}
			changed, err := git.UninstallPostCommit(root)
			if err != nil {
				return err
			}
			if !changed {
				fmt.Fprintln(out, "shiptrace post-commit hook was not installed; nothing to do.")
				return nil
			}
			fmt.Fprintln(out, "✓ shiptrace block removed from post-commit hook.")
			return nil
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Target git repo (default: current working directory)")
	return cmd
}

func newAdapterStatusCommand(out io.Writer) *cobra.Command {
	var repoFlag string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which ship adapters are installed for a repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := resolveRepoFlag(repoFlag)
			if err != nil {
				return err
			}
			root, err := git.FindRepoRoot(repo)
			if err != nil {
				fmt.Fprintln(out, "  not a git repo:", repo)
				return nil
			}
			installed, bin, err := git.IsInstalled(root)
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "repo:", root)
			if installed {
				fmt.Fprintf(out, "  ✓ git post-commit installed (binary: %s)\n", bin)
			} else {
				fmt.Fprintln(out, "  ✗ git post-commit NOT installed (run `shiptrace adapter install git`)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Target git repo (default: current working directory)")
	return cmd
}

func newAdapterPRPollCommand(out, errOut io.Writer) *cobra.Command {
	var (
		repoFlag string
		limit    int
		dryRun   bool
	)
	cmd := &cobra.Command{
		Use:   "pr-poll",
		Short: "Emit ship events for newly-merged PRs (one-shot; cron-friendly)",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := resolveRepoFlag(repoFlag)
			if err != nil {
				return err
			}
			root, err := git.FindRepoRoot(repo)
			if err != nil {
				return err
			}
			prs, err := git.FetchMergedPRs(root, limit)
			if err != nil {
				return err
			}
			home, err := paths.Home()
			if err != nil {
				return err
			}
			statePath := filepath.Join(home, git.PRPollStateFileName)
			state, err := git.LoadPRPollState(statePath)
			if err != nil {
				return err
			}
			fresh := git.FilterNew(state, prs)
			fmt.Fprintf(out, "found %d merged PR(s), %d new\n", len(prs), len(fresh))
			if len(fresh) == 0 {
				return nil
			}
			if dryRun {
				for _, p := range fresh {
					fmt.Fprintf(out, "  would emit: %s — %s\n", p.URL, p.Title)
				}
				return nil
			}
			eventsDir, err := paths.EventsDir()
			if err != nil {
				return err
			}
			w, err := eventlog.New(eventsDir)
			if err != nil {
				return err
			}
			defer w.Close()
			now := time.Now().UTC()
			for _, p := range fresh {
				ev := events.Event{
					EventType: events.Ship,
					Ts:        now,
					Provider:  git.Provider,
					Metadata: map[string]any{
						"kind":      "pr_merged",
						"ref":       p.URL,
						"title":     p.Title,
						"pr_number": p.Number,
						"merged_at": p.MergedAt.Format(time.RFC3339),
						"base_ref":  p.BaseRefName,
					},
				}
				if err := w.Append(ev); err != nil {
					return err
				}
				fmt.Fprintf(out, "  ✓ emitted ship for %s — %s\n", p.URL, p.Title)
			}
			git.MarkSeen(state, fresh)
			return git.SavePRPollState(statePath, state)
		},
	}
	cmd.Flags().StringVar(&repoFlag, "repo", "", "Target git repo (default: current working directory)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max merged PRs to inspect per run")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be emitted without writing events")
	return cmd
}

func resolveRepoFlag(flag string) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return cwd, nil
}

// resolvePostCommitBinary mirrors resolveHookBinary in init.go but for
// the git post-commit binary. The two are kept separate so each adapter
// can grow its own resolution semantics.
func resolvePostCommitBinary(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("--binary not found: %w", err)
		}
		return abs, nil
	}
	if path, err := exec.LookPath(PostCommitBinaryName); err == nil {
		return path, nil
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), PostCommitBinaryName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find %s on PATH or next to shiptrace. Pass --binary <path> or `go install ./cmd/%s`", PostCommitBinaryName, PostCommitBinaryName)
}
