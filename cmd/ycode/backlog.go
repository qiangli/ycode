package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/backlog"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/runtime/projectid"
)

// newBacklogCmd builds the `ycode backlog` command tree. The backlog
// is the markdown-as-source-of-truth task spec at docs/backlog/.
//
// See docs/backlog.md and the Self-Bootstrap section of AGENTS.md.
func newBacklogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backlog",
		Short: "Manage docs/backlog/ — the canonical task list (Foreman-only)",
		Long: `docs/backlog/ holds one .md file per task with YAML frontmatter
(slug, title, priority, state, gitea_issue, acceptance). It is the
source of truth — Gitea issues in admin/<slug> are derived. Wipe
Gitea any time and the reconciler rebuilds it from the markdown.

Requires "ycode serve" to be running (writes ~/.agents/ycode/gitea.url).`,
	}
	cmd.AddCommand(newBacklogListCmd())
	cmd.AddCommand(newBacklogNewCmd())
	cmd.AddCommand(newBacklogReconcileCmd())
	cmd.AddCommand(newBacklogShowCmd())
	return cmd
}

func newBacklogListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List backlog entries from docs/backlog/",
		RunE: func(cmd *cobra.Command, args []string) error {
			priority, _ := cmd.Flags().GetString("priority")
			state, _ := cmd.Flags().GetString("state")
			dir, err := backlogDir()
			if err != nil {
				return err
			}
			items, err := backlog.Load(dir)
			if err != nil {
				slog.Warn("backlog: partial load", "err", err)
			}
			n := 0
			for _, it := range items {
				if priority != "" && priority != "all" && it.Priority != priority {
					continue
				}
				if state != "" && state != "all" && it.State != state {
					continue
				}
				link := "—"
				if it.GiteaIssue != nil {
					link = fmt.Sprintf("#%d", *it.GiteaIssue)
				}
				fmt.Printf("[%s]  %-13s  %-30s  %s\n", it.Priority, it.State, link+"  "+it.Slug, it.Title)
				n++
			}
			if n == 0 {
				fmt.Println("(no entries)")
			}
			return nil
		},
	}
	cmd.Flags().String("priority", "", "Filter by priority: p1|p2|p3 (default: all)")
	cmd.Flags().String("state", "", "Filter by state: open|in_progress|done (default: all)")
	return cmd
}

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "task"
	}
	return s
}

func newBacklogNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new \"<title>\"",
		Short: "Scaffold a new docs/backlog/<slug>.md entry",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := strings.Join(args, " ")
			priority, _ := cmd.Flags().GetString("priority")
			slug, _ := cmd.Flags().GetString("slug")
			body, _ := cmd.Flags().GetString("body")
			if !backlog.IsValidPriority(priority) {
				return fmt.Errorf("invalid priority %q (want p1|p2|p3)", priority)
			}
			if slug == "" {
				slug = slugify(title)
			}
			dir, err := backlogDir()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			path := filepath.Join(dir, slug+".md")
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("already exists: %s", path)
			}
			issue := backlog.Issue{
				Slug:     slug,
				Title:    title,
				Priority: priority,
				State:    backlog.StateOpen,
				Created:  time.Now().UTC(),
				Body:     body,
			}
			if err := backlog.WriteFile(issue, path); err != nil {
				return err
			}
			fmt.Println(path)
			return nil
		},
	}
	cmd.Flags().String("priority", "p2", "Priority: p1|p2|p3")
	cmd.Flags().String("slug", "", "Override slug (default: kebab-case of title)")
	cmd.Flags().String("body", "", "Initial body markdown")
	return cmd
}

func newBacklogReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Sync docs/backlog/ → Gitea (one-shot; serve also runs this on a 60s poll)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, _, c, p, err := openTasks(cmd.Context())
			if err != nil {
				return err
			}
			dir, err := backlogDir()
			if err != nil {
				return err
			}
			log := slog.Default()
			if err := backlog.Reconcile(ctx, dir, c, p, log); err != nil {
				return err
			}
			return nil
		},
	}
}

func newBacklogShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <slug>",
		Short: "Print one backlog entry (frontmatter + body)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := backlogDir()
			if err != nil {
				return err
			}
			path := filepath.Join(dir, args[0]+".md")
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Print(string(data))
			return nil
		},
	}
}

// backlogDir resolves the per-project backlog directory under
// ~/.agents/ycode/projects/<id>/backlog/. The id is the logical
// project id (see projectStateDir), so two checkouts of the same repo
// share a backlog.
func backlogDir() (string, error) {
	stateDir, err := projectStateDir(context.Background())
	if err != nil {
		return "", err
	}
	dir := projectid.BacklogDir(stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// startBacklogReconciler runs an initial Reconcile then a 60s polling
// loop. The caller supplies the resolved backlog directory so this
// function doesn't need to know about path layout.
//
// Errors from Reconcile are logged but do not abort the loop — a
// transient Gitea hiccup must not take down `ycode serve`.
func startBacklogReconciler(ctx context.Context, log *slog.Logger, dir string, c *gitserver.Client, p *projects.Project) error {
	if log == nil {
		log = slog.Default()
	}
	if err := backlog.Reconcile(ctx, dir, c, p, log); err != nil {
		log.Warn("backlog: initial reconcile failed", "err", err)
	}
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := backlog.Reconcile(ctx, dir, c, p, log); err != nil {
					log.Warn("backlog: reconcile poll failed", "err", err)
				}
			}
		}
	}()
	return nil
}
