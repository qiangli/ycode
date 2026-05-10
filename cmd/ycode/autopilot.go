//go:build experimental

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	loompkg "github.com/qiangli/ycode/pkg/loom"
)

// newAutopilotCmd builds `ycode autopilot ...` for the Foreman/Worker
// split. The Worker is a restricted-surface subprocess invoked by the
// Foreman with one issue and one Loom workspace.
func newAutopilotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autopilot",
		Short: "Foreman/Worker autopilot subcommands (experimental)",
	}
	cmd.AddCommand(newAutopilotWorkerCmd())
	return cmd
}

func newAutopilotWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Worker mode: take one issue + Loom workspace and autopilot it",
		Long: `The Worker is the inner agent in the Boss → Foreman → Worker chain.
It receives exactly two inputs from the Foreman:

  --issue    Gitea issue number on admin/<slug> in the embedded Gitea
  --loom-id  Loom workspace lease ID (from mcp__ycode-loom__loom_lease)

It fetches the issue title+body, cd's into the Loom workspace path,
and runs the existing /autopilot collab inner loop (RESEARCH → PLAN →
BUILD → TEST → FIX → COMMIT) within that sandbox.

v1 privilege boundary: the Worker has no docs/backlog/, no Foreman CLI
(backlog/foreman/serve), and only the Gitea + Loom tools its task
requires. Strict in-process MCP-tool filtering is a follow-up.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			issueNum, _ := cmd.Flags().GetInt64("issue")
			loomID, _ := cmd.Flags().GetString("loom-id")
			repo, _ := cmd.Flags().GetString("repo")
			if issueNum <= 0 {
				return fmt.Errorf("--issue is required")
			}
			if loomID == "" {
				return fmt.Errorf("--loom-id is required")
			}
			return runWorker(cmd.Context(), issueNum, loomID, repo)
		},
	}
	cmd.Flags().Int64("issue", 0, "Gitea issue number to work on (required)")
	cmd.Flags().String("loom-id", "", "Loom workspace lease ID (required)")
	cmd.Flags().String("repo", "", "Override repo as owner/name (default: project repo)")
	return cmd
}

// runWorker resolves the loom workspace + issue and shells out to the
// existing /autopilot collab inner loop within the workspace.
func runWorker(ctx context.Context, issueNum int64, loomID, repoOverride string) error {
	// 1. Resolve Loom workspace path.
	lease, err := lookupLoomLease(loomID)
	if err != nil {
		return fmt.Errorf("worker: resolve loom workspace: %w", err)
	}
	if _, err := os.Stat(lease.Path); err != nil {
		return fmt.Errorf("worker: loom path %s missing: %w", lease.Path, err)
	}

	// 2. Resolve Gitea connection via the discovery files written by serve.
	baseURL, err := readDiscoveryURL()
	if err != nil {
		return fmt.Errorf("worker: gitea discovery: %w", err)
	}
	token, err := readDiscoveryToken()
	if err != nil {
		return fmt.Errorf("worker: gitea token: %w", err)
	}
	c := gitserver.NewClient(baseURL, token)

	// 3. Resolve repo (owner/name).
	owner, repoName, err := resolveWorkerRepo(ctx, c, lease, repoOverride)
	if err != nil {
		return err
	}

	// 4. Fetch issue title+body.
	issue, err := c.GetIssue(ctx, owner, repoName, issueNum)
	if err != nil {
		return fmt.Errorf("worker: get issue #%d in %s/%s: %w", issueNum, owner, repoName, err)
	}

	// 5. Hand off to the existing /autopilot collab inner loop. This is
	// the same path collab.Orchestrator.runAutopilot uses (see
	// internal/gitserver/collab/orchestrator.go:309), which means the
	// autopilot skill is reused unchanged. The privilege boundary here
	// is conventional in v1: the Worker subprocess only has the
	// inherited environment and the cwd locked to the Loom workspace.
	prompt := fmt.Sprintf("/autopilot collab task %s\n\n%s", issue.Title, issue.Body)
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("worker: locate ycode bin: %w", err)
	}
	child := exec.CommandContext(ctx, bin, "prompt", prompt)
	child.Dir = lease.Path
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	child.Stdin = os.Stdin
	child.Env = append(os.Environ(), "YCODE_WORKER=1", "YCODE_WORKER_ISSUE="+strconv.FormatInt(issueNum, 10), "YCODE_WORKER_LOOM_ID="+loomID)

	fmt.Fprintf(os.Stderr, "worker: issue=#%d loom=%s path=%s\n", issueNum, loomID, lease.Path)
	if err := child.Run(); err != nil {
		return fmt.Errorf("worker: autopilot exit: %w", err)
	}
	return nil
}

// lookupLoomLease reads the loom lease store directly. The Worker
// subprocess runs on the same host as the Foreman and has filesystem
// access to the gitea data dir.
func lookupLoomLease(loomID string) (loompkg.Lease, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return loompkg.Lease{}, err
	}
	leasePath := filepath.Join(home, ".agents", "ycode", "gitea", "loom", "leases.json")
	store, err := loompkg.NewFileStore(leasePath)
	if err != nil {
		return loompkg.Lease{}, fmt.Errorf("loom store: %w", err)
	}
	lease, ok := store.Get(loomID)
	if !ok {
		return loompkg.Lease{}, fmt.Errorf("loom: lease %q not found", loomID)
	}
	return lease, nil
}

// resolveWorkerRepo decides which Gitea repo the worker reads from.
// Priority: --repo override → lease.Slug (admin/<slug>).
func resolveWorkerRepo(_ context.Context, _ *gitserver.Client, lease loompkg.Lease, override string) (owner, repo string, err error) {
	if override != "" {
		parts := strings.SplitN(override, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("--repo must be owner/name, got %q", override)
		}
		return parts[0], parts[1], nil
	}
	if lease.Slug == "" {
		return "", "", fmt.Errorf("worker: loom lease has no slug; pass --repo")
	}
	return projects.Owner, lease.Slug, nil
}
