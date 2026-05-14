package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/adapters/filesystem"
	"github.com/LaurPl/shiptrace/internal/adapters/git"
	"github.com/LaurPl/shiptrace/internal/config"
	"github.com/LaurPl/shiptrace/internal/eventlog"
	"github.com/LaurPl/shiptrace/internal/events"
	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/store"
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
		newAdapterScanFSCommand(out, errOut),
	)
	return cmd
}

func newAdapterScanFSCommand(out, _ io.Writer) *cobra.Command {
	var (
		projectFilter string
		extraPaths    []string
		dryRun        bool
	)
	cmd := &cobra.Command{
		Use:   "scan-fs",
		Short: "Scan configured ship_paths for new files and emit attributed ship events",
		Long:  "Reads ~/.shiptrace/config.yaml and walks each project's ship_paths. Files modified since the last scan are emitted as kind=file_landed ship events with file_overlap or time_window attribution.",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := paths.Home()
			if err != nil {
				return err
			}
			configPath := filepath.Join(home, config.FileName)
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			shipPaths, projectNames := collectShipPaths(cfg, projectFilter)
			shipPaths = append(shipPaths, extraPaths...)
			if len(shipPaths) == 0 {
				fmt.Fprintln(out, "no ship_paths configured (and no --path flags) — nothing to scan")
				fmt.Fprintln(out, "  edit", configPath, "to add ship_paths under projects.<name>")
				return nil
			}
			statePath := filepath.Join(home, filesystem.StateFileName)
			state, err := filesystem.LoadState(statePath)
			if err != nil {
				return err
			}
			matches, err := filesystem.Scan(state, shipPaths)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "scanned %d ship_path(s); %d new/changed file(s)\n", len(shipPaths), len(matches))
			if len(matches) == 0 {
				return nil
			}
			if dryRun {
				for _, m := range matches {
					fmt.Fprintf(out, "  would emit: %s (mtime %s)\n", m.Path, m.Mtime.Format(time.RFC3339))
				}
				return nil
			}

			dbPath, err := paths.DBPath()
			if err != nil {
				return err
			}
			s, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			eventsDir, err := paths.EventsDir()
			if err != nil {
				return err
			}
			w, err := eventlog.New(eventsDir)
			if err != nil {
				return err
			}
			defer w.Close()

			emitted, err := filesystem.EmitShipEvents(cmd.Context(), w, s, matches, time.Now().UTC())
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "  emitted %d ship event(s)", emitted)
			if len(projectNames) > 0 {
				fmt.Fprintf(out, " from project(s): %v", projectNames)
			}
			fmt.Fprintln(out)

			filesystem.MarkSeen(state, matches)
			return filesystem.SaveState(statePath, state)
		},
	}
	cmd.Flags().StringVar(&projectFilter, "project", "", "Limit scan to one project's ship_paths (default: all projects)")
	cmd.Flags().StringSliceVar(&extraPaths, "path", nil, "Additional path or glob to scan (can repeat)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would be emitted without writing events")
	return cmd
}

// collectShipPaths returns the union of configured ship_paths (filtered
// by --project when set) plus the names of the projects contributing
// them. The names list is used to enrich the user-facing summary line.
func collectShipPaths(cfg *config.Config, projectFilter string) ([]string, []string) {
	if cfg == nil {
		return nil, nil
	}
	var paths []string
	var names []string
	for name, p := range cfg.Projects {
		if projectFilter != "" && name != projectFilter {
			continue
		}
		if len(p.ShipPaths) == 0 {
			continue
		}
		paths = append(paths, p.ShipPaths...)
		names = append(names, name)
	}
	return paths, names
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
