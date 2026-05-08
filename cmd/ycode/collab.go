package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/collab"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

// newCollabCmd builds the `ycode collab` command — runs the autopilot
// collab orchestrator against the running Gitea. See docs/agent-collab.md.
func newCollabCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collab",
		Short: "Run N autopilot agents against the queued tasks for this project",
		Long: `Spawn N child autopilot processes, each working a popped task in an
isolated fork checkout of admin/<slug> on the embedded Gitea. The merger
auto-merges PRs whose CI command exits 0. The user's working tree is
never modified — sync merged work back via "ycode tasks pull".

Requires "ycode serve" to be running (writes ~/.agents/ycode/gitea.url).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			numAgents, _ := cmd.Flags().GetInt("agents")
			ciCommand, _ := cmd.Flags().GetString("ci")
			issueTimeoutSec, _ := cmd.Flags().GetInt("issue-timeout-seconds")
			pollSec, _ := cmd.Flags().GetInt("poll-seconds")

			ctx, _, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()
			home, _ := os.UserHomeDir()
			dataDir := filepath.Join(home, ".agents", "ycode", "gitea")

			// Mirror cwd HEAD to upstream before agents start working
			// against it. Without this, agents would clone an outdated
			// or empty repo.
			if err := mirrorBeforeCollab(ctx, c, p, cwd); err != nil {
				return fmt.Errorf("collab: mirror cwd: %w", err)
			}

			cloneURL, err := lookupCloneURL(ctx, c, p)
			if err != nil {
				return err
			}
			token, err := readDiscoveryToken()
			if err != nil {
				return err
			}
			syncLog, err := projects.NewSyncLog(dataDir, p)
			if err != nil {
				return err
			}

			o, err := collab.New(collab.Config{
				Project:      p,
				Client:       c,
				SyncLog:      syncLog,
				NumAgents:    numAgents,
				CICommand:    ciCommand,
				YcodeBin:     os.Args[0],
				SandboxRoot:  filepath.Join(dataDir, "collab-sandboxes"),
				SessionsRoot: filepath.Join(dataDir, "collab-sessions"),
				IssueTimeout: time.Duration(issueTimeoutSec) * time.Second,
				PollInterval: time.Duration(pollSec) * time.Second,
				Token:        token,
				CloneURL:     cloneURL,
				HostCwd:      cwd,
				Logger:       slog.Default(),
			})
			if err != nil {
				return err
			}

			fmt.Printf("collab: %d agent(s) running on %s (cwd=%s)\n", numAgents, p.Slug, cwd)
			fmt.Printf("        sandboxes: %s\n", filepath.Join(dataDir, "collab-sandboxes"))
			fmt.Printf("        sessions:  %s\n", filepath.Join(dataDir, "collab-sessions"))
			fmt.Printf("        Gitea UI:  %s/admin/%s\n", trimURL(cloneURL), p.Slug)
			fmt.Printf("        Ctrl-C to stop.\n")

			runCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			sigs := make(chan os.Signal, 1)
			signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigs
				fmt.Println("\ncollab: shutting down...")
				cancel()
			}()

			return o.Run(runCtx)
		},
	}
	cmd.Flags().Int("agents", 1, "Number of agents to run in parallel")
	cmd.Flags().String("ci", "", "CI command that gates auto-merge (default: settings.tasks.ciCommand or unconditional merge)")
	cmd.Flags().Int("issue-timeout-seconds", 1800, "Max wall-clock per autopilot run (0 = orchestrator default)")
	cmd.Flags().Int("poll-seconds", 5, "How often to poll the queue and merger PRs")
	return cmd
}

// mirrorBeforeCollab is the same as `ycode tasks mirror` but inline.
// Idempotent.
func mirrorBeforeCollab(ctx context.Context, c *gitserver.Client, p *projects.Project, cwd string) error {
	cloneURL, err := lookupCloneURL(ctx, c, p)
	if err != nil {
		return err
	}
	token, err := readDiscoveryToken()
	if err != nil {
		return err
	}
	if err := projects.MirrorUpstream(ctx, cwd, projects.MirrorOptions{
		CloneURL: cloneURL,
		Token:    token,
		Force:    true,
	}); err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	r, _ := projects.NewRegistry(filepath.Join(home, ".agents", "ycode", "gitea"))
	_ = r.MarkSynced(cwd)
	return nil
}

func lookupCloneURL(ctx context.Context, c *gitserver.Client, p *projects.Project) (string, error) {
	repos, err := c.ListRepos(ctx)
	if err != nil {
		return "", err
	}
	for _, r := range repos {
		if r.Name == p.Slug {
			return r.CloneURL, nil
		}
	}
	return "", fmt.Errorf("collab: tracking repo admin/%s not found in Gitea", p.Slug)
}

// trimURL drops the trailing "/<repo>.git" so we can print a clickable
// repo URL pointing at the Gitea web UI.
func trimURL(cloneURL string) string {
	if i := lastSlash(cloneURL); i > 0 {
		return cloneURL[:i]
	}
	return cloneURL
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
