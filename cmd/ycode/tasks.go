package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/merger"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
)

// newTasksCmd builds the `ycode tasks` command tree for the multi-agent
// collaboration workflow. See docs/agent-collab.md.
func newTasksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "Manage the multi-agent task queue (issues in internal Gitea)",
		Long: `Tasks are issues in admin/<slug> on the embedded Gitea server.
Agents pop the highest-priority unclaimed task and open PRs back to main;
the merger auto-merges on green local CI. See docs/agent-collab.md.

Requires "ycode serve" to be running (writes ~/.agents/ycode/gitea.url).`,
	}

	cmd.AddCommand(newTasksAddCmd())
	cmd.AddCommand(newTasksListCmd())
	cmd.AddCommand(newTasksPullCmd())
	cmd.AddCommand(newTasksMirrorCmd())
	cmd.AddCommand(newTasksTickCmd())
	return cmd
}

func newTasksAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [title]",
		Short: "Add a task to the queue",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			body, _ := cmd.Flags().GetString("body")
			priority, _ := cmd.Flags().GetString("priority")
			autoMerge, _ := cmd.Flags().GetBool("auto-merge")
			pushOrigin, _ := cmd.Flags().GetBool("push-origin")

			ctx, cwd, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}
			_ = cwd

			if err := queue.EnsureLabels(ctx, c, p); err != nil {
				return err
			}
			labels := []string{}
			if autoMerge {
				labels = append(labels, queue.LabelAutoMerge)
			}
			if pushOrigin {
				labels = append(labels, queue.LabelPushOrigin)
			}
			issue, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
				Title:    title,
				Body:     body,
				Priority: priority,
				Labels:   labels,
			})
			if err != nil {
				return err
			}
			fmt.Printf("#%d  [%s]  %s\n", issue.Number, queue.Priority(issue), issue.Title)
			return nil
		},
	}
	cmd.Flags().String("body", "", "Issue body / extended description")
	cmd.Flags().String("priority", "p2", "Priority: p1|p2|p3")
	cmd.Flags().Bool("auto-merge", false, "Auto-merge once CI is green")
	cmd.Flags().Bool("push-origin", false, "Post-merge: push to host repo's origin")
	return cmd
}

func newTasksListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks in the queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, _ := cmd.Flags().GetString("state")
			ctx, _, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}
			issues, err := queue.List(ctx, c, p, state)
			if err != nil {
				return err
			}
			if len(issues) == 0 {
				fmt.Println("(no tasks)")
				return nil
			}
			for _, i := range issues {
				claim := queue.ClaimedBy(&i)
				if claim == "" {
					claim = "—"
				}
				fmt.Printf("#%-4d  [%s]  state=%-7s claim=%-20s  %s\n",
					i.Number, queue.Priority(&i), i.State, claim, i.Title)
			}
			return nil
		},
	}
	cmd.Flags().String("state", "open", "open|closed|all")
	return cmd
}

func newTasksPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fast-forward cwd from internal upstream/main (no auto-stash, no auto-rebase)",
		Long: `Pull merges that landed in admin/<slug>:main into the user's cwd.

Refuses on:
  - dirty working tree (uncommitted changes)
  - non-fast-forward (cwd has diverged)

This is the only sanctioned channel for syncing agent work back to cwd.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cwd, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			dataDir := filepath.Join(home, ".agents", "ycode", "gitea")
			synclog, err := projects.NewSyncLog(dataDir, p)
			if err != nil {
				return err
			}
			pending, err := synclog.Pending()
			if err != nil {
				return err
			}
			if len(pending) == 0 {
				fmt.Println("nothing to pull")
				return nil
			}

			cloneURL, err := lookupCloneURL(ctx, c, p)
			if err != nil {
				return fmt.Errorf("tasks pull: %w", err)
			}
			token, err := readDiscoveryToken()
			if err != nil {
				return err
			}
			if err := projects.PullFastForward(ctx, cwd, cloneURL, token); err != nil {
				return fmt.Errorf("tasks pull: %w", err)
			}
			if err := synclog.Truncate(); err != nil {
				slog.Warn("tasks pull: synclog truncate", "err", err)
			}
			fmt.Printf("pulled %d merged change(s) into %s\n", len(pending), cwd)
			return nil
		},
	}
	return cmd
}

func newTasksMirrorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mirror",
		Short: "Push cwd HEAD to internal upstream (creates the tracking repo if missing)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cwd, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}
			created, err := projects.EnsureRepo(ctx, c, p)
			if err != nil {
				return err
			}
			if created {
				fmt.Printf("created admin/%s\n", p.Slug)
			}
			repos, err := c.ListRepos(ctx)
			if err != nil {
				return err
			}
			var cloneURL string
			for _, r := range repos {
				if r.Name == p.Slug {
					cloneURL = r.CloneURL
					break
				}
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
			fmt.Printf("mirrored %s -> admin/%s:main\n", cwd, p.Slug)
			return nil
		},
	}
}

func newTasksTickCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tick",
		Short: "Run one merger pass (CI on each open PR, auto-merge on green)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ciCmd, _ := cmd.Flags().GetString("ci")
			ctx, _, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}
			repos, err := c.ListRepos(ctx)
			if err != nil {
				return err
			}
			var cloneURL string
			for _, r := range repos {
				if r.Name == p.Slug {
					cloneURL = r.CloneURL
					break
				}
			}
			if cloneURL == "" {
				return fmt.Errorf("tasks tick: tracking repo admin/%s not found", p.Slug)
			}
			token, err := readDiscoveryToken()
			if err != nil {
				return err
			}
			home, _ := os.UserHomeDir()
			dataDir := filepath.Join(home, ".agents", "ycode", "gitea")
			synclog, err := projects.NewSyncLog(dataDir, p)
			if err != nil {
				return err
			}
			m, err := merger.New(merger.Config{
				Client:    c,
				Project:   p,
				SyncLog:   synclog,
				CloneURL:  cloneURL,
				Token:     token,
				CICommand: ciCmd,
				WorkDir:   filepath.Join(dataDir, "merger-work"),
				Logger:    slog.Default(),
			})
			if err != nil {
				return err
			}
			return m.Tick(ctx)
		},
	}
	cmd.Flags().String("ci", "", "CI command to gate auto-merge (default: settings.tasks.ciCommand)")
	return cmd
}

// --- helpers ---

// openTasks resolves the project for cwd, returns an authenticated client,
// and a context. Errors clearly when serve isn't running.
func openTasks(ctx context.Context) (context.Context, string, *gitserver.Client, *projects.Project, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ctx, "", nil, nil, err
	}
	baseURL, err := readDiscoveryURL()
	if err != nil {
		return ctx, cwd, nil, nil, err
	}
	token, err := readDiscoveryToken()
	if err != nil {
		return ctx, cwd, nil, nil, err
	}
	c := gitserver.NewClient(baseURL, token)
	home, _ := os.UserHomeDir()
	r, err := projects.NewRegistry(filepath.Join(home, ".agents", "ycode", "gitea"))
	if err != nil {
		return ctx, cwd, nil, nil, err
	}
	p, err := r.Resolve(ctx, cwd)
	if err != nil {
		return ctx, cwd, nil, nil, err
	}
	if _, err := projects.EnsureRepo(ctx, c, p); err != nil {
		return ctx, cwd, nil, nil, fmt.Errorf("tasks: ensure repo: %w", err)
	}
	return ctx, cwd, c, p, nil
}

func readDiscoveryURL() (string, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".agents", "ycode", "gitea.url")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("ycode tasks requires `ycode serve` to be running (missing %s)", path)
		}
		return "", err
	}
	u := strings.TrimSpace(string(data))
	if _, err := url.Parse(u); err != nil {
		return "", fmt.Errorf("malformed gitea.url: %w", err)
	}
	return u, nil
}

func readDiscoveryToken() (string, error) {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".agents", "ycode", "gitea.token")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("missing gitea token at %s — set gitServer.token in settings.json and restart serve", path)
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
