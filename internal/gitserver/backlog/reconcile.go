package backlog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/queue"
)

// Reconcile synchronizes docs/backlog/*.md into Gitea issues in
// admin/<project-slug>. Direction:
//
//   - markdown → Gitea: create missing issues, update drifted
//     title/body/priority labels, close issues for done markdown.
//   - Gitea → markdown: closed Gitea issues whose markdown is still
//     open|in_progress get their state flipped to "done" (this is how
//     Worker completion propagates back to the source of truth).
//
// Reconcile is monotonic — it never deletes on either side. Orphan
// Gitea issues from deleted markdown stay around (acceptable v1).
//
// Safe to call concurrently with itself: every step is idempotent and
// the underlying queue.Submit / c.UpdateIssue calls are individually
// race-tolerant. The reconciler does NOT take a lock on docs/backlog/.
func Reconcile(ctx context.Context, dir string, c *gitserver.Client, p *projects.Project, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if err := queue.EnsureLabels(ctx, c, p); err != nil {
		return fmt.Errorf("backlog reconcile: ensure labels: %w", err)
	}
	mdIssues, loadErr := Load(dir)
	if loadErr != nil {
		log.Warn("backlog: partial load", "err", loadErr)
	}
	giteaIssues, err := c.ListIssues(ctx, projects.Owner, p.Slug, "all", nil)
	if err != nil {
		return fmt.Errorf("backlog reconcile: list issues: %w", err)
	}

	bySlug := indexBySlug(giteaIssues)
	byNumber := indexByNumber(giteaIssues)

	created, updated, closed, mirrored := 0, 0, 0, 0

	for i := range mdIssues {
		md := mdIssues[i]
		gi := matchMarkdownToGitea(md, bySlug, byNumber)
		switch {
		case gi == nil:
			// Need to create. Submit + writeback the new issue number.
			submitted, err := queue.Submit(ctx, c, p, queue.SubmitOptions{
				Title:    md.Title,
				Body:     RenderGiteaBody(md),
				Priority: md.Priority,
			})
			if err != nil {
				log.Error("backlog: submit", "slug", md.Slug, "err", err)
				continue
			}
			if err := SetGiteaIssue(dir, md.Slug, submitted.Number); err != nil {
				log.Error("backlog: writeback gitea_issue", "slug", md.Slug, "issue", submitted.Number, "err", err)
				// non-fatal: next reconcile will re-link via slug marker
			}
			created++
			log.Info("backlog: created", "slug", md.Slug, "issue", submitted.Number, "priority", md.Priority)

		default:
			// Re-link if frontmatter lost its gitea_issue.
			if md.GiteaIssue == nil {
				if err := SetGiteaIssue(dir, md.Slug, gi.Number); err != nil {
					log.Warn("backlog: relink gitea_issue", "slug", md.Slug, "issue", gi.Number, "err", err)
				}
			}
			// Title / body / priority drift → UpdateIssue.
			expectedBody := RenderGiteaBody(md)
			titleDrift := gi.Title != md.Title
			bodyDrift := normalizeBody(gi.Body) != normalizeBody(expectedBody)
			priorityDrift := queue.Priority(gi) != md.Priority
			if titleDrift || bodyDrift || priorityDrift {
				patchUpdates := map[string]any{}
				if titleDrift {
					patchUpdates["title"] = md.Title
				}
				if bodyDrift {
					patchUpdates["body"] = expectedBody
				}
				if len(patchUpdates) > 0 {
					if _, err := c.UpdateIssue(ctx, projects.Owner, p.Slug, gi.Number, patchUpdates); err != nil {
						log.Error("backlog: update issue", "slug", md.Slug, "issue", gi.Number, "err", err)
						continue
					}
				}
				if priorityDrift {
					if err := setPriorityLabel(ctx, c, p, gi, md.Priority); err != nil {
						log.Error("backlog: update priority", "slug", md.Slug, "issue", gi.Number, "err", err)
						continue
					}
				}
				updated++
				log.Info("backlog: updated", "slug", md.Slug, "issue", gi.Number, "title_drift", titleDrift, "body_drift", bodyDrift, "priority_drift", priorityDrift)
			}
			// Close Gitea if markdown is done but Gitea is still open.
			if md.State == StateDone && gi.State != "closed" {
				if err := queue.Complete(ctx, c, p, gi.Number); err != nil {
					log.Error("backlog: close issue", "slug", md.Slug, "issue", gi.Number, "err", err)
					continue
				}
				closed++
				log.Info("backlog: closed", "slug", md.Slug, "issue", gi.Number)
			}
			// Mirror Gitea-closed back to markdown done.
			if gi.State == "closed" && md.State != StateDone {
				if err := MarkState(dir, md.Slug, StateDone); err != nil {
					log.Error("backlog: mark done", "slug", md.Slug, "err", err)
					continue
				}
				mirrored++
				log.Info("backlog: mirrored close", "slug", md.Slug, "issue", gi.Number)
			}
		}
	}

	log.Info("backlog: reconciled",
		"dir", dir,
		"repo", fmt.Sprintf("%s/%s", projects.Owner, p.Slug),
		"markdown_count", len(mdIssues),
		"gitea_count", len(giteaIssues),
		"created", created,
		"updated", updated,
		"closed", closed,
		"mirrored_back", mirrored,
	)
	return nil
}

// matchMarkdownToGitea returns the Gitea issue corresponding to the
// markdown, looking up by GiteaIssue number first and falling back to
// the slug marker in the body.
func matchMarkdownToGitea(md Issue, bySlug map[string]*gitserver.Issue, byNumber map[int64]*gitserver.Issue) *gitserver.Issue {
	if md.GiteaIssue != nil {
		if gi, ok := byNumber[*md.GiteaIssue]; ok {
			return gi
		}
		// Number stale (e.g. after Gitea wipe) — fall through to slug match.
	}
	if gi, ok := bySlug[md.Slug]; ok {
		return gi
	}
	return nil
}

func indexBySlug(issues []gitserver.Issue) map[string]*gitserver.Issue {
	out := make(map[string]*gitserver.Issue, len(issues))
	for i := range issues {
		slug := SlugFromGiteaBody(issues[i].Body)
		if slug == "" {
			continue
		}
		out[slug] = &issues[i]
	}
	return out
}

func indexByNumber(issues []gitserver.Issue) map[int64]*gitserver.Issue {
	out := make(map[int64]*gitserver.Issue, len(issues))
	for i := range issues {
		out[issues[i].Number] = &issues[i]
	}
	return out
}

// setPriorityLabel swaps whatever priority label is on issue for the
// requested one, leaving non-priority labels alone.
func setPriorityLabel(ctx context.Context, c *gitserver.Client, p *projects.Project, gi *gitserver.Issue, priority string) error {
	keep := make([]string, 0, len(gi.Labels)+1)
	for _, l := range gi.Labels {
		switch l.Name {
		case queue.LabelP1, queue.LabelP2, queue.LabelP3:
			continue
		default:
			keep = append(keep, l.Name)
		}
	}
	keep = append(keep, priority)
	// Resolve names → IDs via a label list call. UpdateIssue's labels
	// field expects integer IDs (per queue.Submit's comment).
	labels, err := c.ListLabels(ctx, projects.Owner, p.Slug)
	if err != nil {
		return err
	}
	byName := make(map[string]int64, len(labels))
	for _, l := range labels {
		byName[l.Name] = l.ID
	}
	ids := make([]int64, 0, len(keep))
	for _, n := range keep {
		if id, ok := byName[n]; ok {
			ids = append(ids, id)
		}
	}
	_, err = c.UpdateIssue(ctx, projects.Owner, p.Slug, gi.Number, map[string]any{
		"labels": ids,
	})
	return err
}

// normalizeBody strips trailing whitespace and CR so reconciler doesn't
// flap on trivial line-ending differences.
func normalizeBody(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, " \t\n")
}
