package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/runtime/projectid"
	"github.com/qiangli/ycode/internal/runtime/selfheal/worker"
	"github.com/qiangli/ycode/internal/runtime/selfheal/workspace"
)

// newAutopilotSelfHealWorkerCmd is the sibling to `worker` for the
// selfheal flow. Takes a signature instead of a Gitea issue + Loom
// lease, since selfheal workspaces are per-signature clones of the
// upstream ycode repo (not Loom-leased project branches).
func newAutopilotSelfHealWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "selfheal-worker",
		Short: "Selfheal worker: run autoloop against one signature's per-fix workspace",
		Long: `The selfheal worker is the inner agent dispatched by the selfheal
daemon when a qualifying tool-failure signature is captured. It:

  1. Reads the signature's docs/backlog/selfheal-<sig>-<tool>.md entry
  2. Materializes a workspace under ~/.agents/ycode/selfheal/<sig>/
     (clone + worktree)
  3. Runs autoloop.Loop with the failure context as the goal
  4. Writes outcome.json with the result

Used by the daemon under ycode serve but also runnable by hand for
re-attempting a specific signature.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			sig, _ := cmd.Flags().GetString("signature")
			baseDir, _ := cmd.Flags().GetString("base-dir")
			backlogDir, _ := cmd.Flags().GetString("backlog-dir")
			repoURL, _ := cmd.Flags().GetString("repo-url")
			if sig == "" {
				return fmt.Errorf("--signature is required")
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			if baseDir == "" {
				baseDir = filepath.Join(home, ".agents", "ycode", "selfheal")
			}
			if backlogDir == "" {
				dir, err := projectStateDir(cmd.Context())
				if err != nil {
					return fmt.Errorf("--backlog-dir required (project id resolution failed: %w)", err)
				}
				backlogDir = projectid.BacklogDir(dir)
			}
			if repoURL == "" {
				cwd, _ := os.Getwd()
				resolved, err := workspace.New().DiscoverFork(cmd.Context(), cwd)
				if err != nil {
					return fmt.Errorf("discover repo URL: %w", err)
				}
				repoURL = resolved
			}
			return runSelfHealWorker(cmd.Context(), sig, baseDir, backlogDir, repoURL)
		},
	}
	cmd.Flags().String("signature", "", "Signature hex from the selfheal backlog entry (required)")
	cmd.Flags().String("base-dir", "", "Workspace root (default ~/.agents/ycode/selfheal)")
	cmd.Flags().String("backlog-dir", "", "Backlog dir (default per-project)")
	cmd.Flags().String("repo-url", "", "ycode source repo URL (default: auto-discover fork → upstream)")
	return cmd
}

func runSelfHealWorker(ctx context.Context, signature, baseDir, backlogDir, repoURL string) error {
	cfg := worker.Config{
		BaseDir:    baseDir,
		BacklogDir: backlogDir,
		RepoURL:    repoURL,
	}
	w, err := worker.New(cfg, signature)
	if err != nil {
		return err
	}
	out, err := w.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "selfheal-worker: error: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "selfheal-worker: done signature=%s mode=%s iterations=%d diff_lines=%d\n",
		out.Signature, out.Mode, out.Iterations, out.DiffLines)
	if out.WorktreePath != "" {
		fmt.Fprintf(os.Stderr, "selfheal-worker: worktree=%s\n", out.WorktreePath)
	}
	if out.Notes != "" {
		fmt.Fprintf(os.Stderr, "selfheal-worker: notes: %s\n", out.Notes)
	}
	if out.Mode != "success" {
		return fmt.Errorf("worker outcome %q", out.Mode)
	}
	return nil
}
