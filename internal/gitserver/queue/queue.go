// Package queue provides a prioritized task queue backed by Gitea issues.
//
// Tasks are issues in admin/<slug>; labels carry priority and claim state:
//
//	p1, p2, p3            // priority (lower = more urgent), default p2
//	in-progress           // soft claim — set when an agent picks it up
//	claimed:<agent-id>    // who is working on it
//	auto-merge            // opt-in for auto-merge once CI is green (otherwise reads project default)
//	push:origin           // post-merge action: push merged SHA to the host repo's "origin" remote
//
// See docs/agent-collab.md.
package queue

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

// Standard label names used by the queue.
const (
	LabelP1          = "p1"
	LabelP2          = "p2"
	LabelP3          = "p3"
	LabelInProgress  = "in-progress"
	LabelAutoMerge   = "auto-merge"
	LabelPushOrigin  = "push:origin"
	LabelClaimPrefix = "claimed:"
)

// PriorityLabels lists the priority labels in order (highest urgency first).
func PriorityLabels() []string { return []string{LabelP1, LabelP2, LabelP3} }

// LabelsToInit are the labels the queue ensures exist in a project repo.
func LabelsToInit() []string {
	return []string{LabelP1, LabelP2, LabelP3, LabelInProgress, LabelAutoMerge, LabelPushOrigin}
}

// EnsureLabels creates the standard set of labels in admin/<slug>.
// Idempotent: errors that look like "already exists" are ignored.
func EnsureLabels(ctx context.Context, c *gitserver.Client, p *projects.Project) error {
	colors := map[string]string{
		LabelP1:         "d73a4a", // red
		LabelP2:         "fbca04", // yellow
		LabelP3:         "0e8a16", // green
		LabelInProgress: "1d76db", // blue
		LabelAutoMerge:  "5319e7", // purple
		LabelPushOrigin: "ededed", // grey
	}
	for _, name := range LabelsToInit() {
		if _, err := c.CreateLabel(ctx, projects.Owner, p.Slug, name, colors[name]); err != nil {
			if !isAlreadyExists(err) {
				return fmt.Errorf("queue: ensure label %s: %w", name, err)
			}
		}
	}
	return nil
}

// SubmitOptions controls how a new task is filed.
type SubmitOptions struct {
	Title    string
	Body     string
	Priority string   // p1|p2|p3 (default p2)
	Labels   []string // additional labels (auto-merge, push:origin, etc.)
}

// Submit files a new task as a Gitea issue.
func Submit(ctx context.Context, c *gitserver.Client, p *projects.Project, opts SubmitOptions) (*gitserver.Issue, error) {
	if opts.Title == "" {
		return nil, fmt.Errorf("queue: empty title")
	}
	pr := opts.Priority
	if pr == "" {
		pr = LabelP2
	}
	if !validPriority(pr) {
		return nil, fmt.Errorf("queue: invalid priority %q", pr)
	}
	labels := append([]string{pr}, opts.Labels...)
	issue, err := c.CreateIssue(ctx, projects.Owner, p.Slug, opts.Title, opts.Body, labels)
	if err != nil {
		return nil, fmt.Errorf("queue: submit: %w", err)
	}
	return issue, nil
}

// Pop atomically claims the highest-priority open, unclaimed issue for the
// given agent ID. Returns nil, nil if no work is available.
//
// "Atomic" here means optimistic: we PATCH the issue with a claim label;
// if two agents race they may both succeed but each will end up working on
// a different issue (or on the same one — the merger rejects duplicates).
func Pop(ctx context.Context, c *gitserver.Client, p *projects.Project, agentID string) (*gitserver.Issue, error) {
	if agentID == "" {
		return nil, fmt.Errorf("queue: empty agentID")
	}
	open, err := c.ListIssues(ctx, projects.Owner, p.Slug, "open", nil)
	if err != nil {
		return nil, fmt.Errorf("queue: list open: %w", err)
	}
	candidates := filterClaimable(open)
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return priorityRank(&candidates[i]) < priorityRank(&candidates[j])
	})
	pick := candidates[0]

	claim := LabelClaimPrefix + agentID
	newLabels := labelNames(pick.Labels)
	newLabels = append(newLabels, LabelInProgress, claim)
	updated, err := c.UpdateIssue(ctx, projects.Owner, p.Slug, pick.Number, map[string]any{
		"labels": uniqueLabelIDs(ctx, c, p, newLabels),
	})
	if err != nil {
		return nil, fmt.Errorf("queue: claim issue %d: %w", pick.Number, err)
	}
	return updated, nil
}

// Release removes the in-progress and claim labels, returning the issue to
// the pool. Use when an agent voluntarily abandons work.
func Release(ctx context.Context, c *gitserver.Client, p *projects.Project, issueNo int64, agentID string) error {
	issue, err := c.GetIssue(ctx, projects.Owner, p.Slug, issueNo)
	if err != nil {
		return fmt.Errorf("queue: get issue %d: %w", issueNo, err)
	}
	keep := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		if l.Name == LabelInProgress {
			continue
		}
		if strings.HasPrefix(l.Name, LabelClaimPrefix) && (agentID == "" || l.Name == LabelClaimPrefix+agentID) {
			continue
		}
		keep = append(keep, l.Name)
	}
	_, err = c.UpdateIssue(ctx, projects.Owner, p.Slug, issueNo, map[string]any{
		"labels": uniqueLabelIDs(ctx, c, p, keep),
	})
	if err != nil {
		return fmt.Errorf("queue: release issue %d: %w", issueNo, err)
	}
	return nil
}

// Complete closes the issue once its PR is merged. The merger calls this.
func Complete(ctx context.Context, c *gitserver.Client, p *projects.Project, issueNo int64) error {
	_, err := c.UpdateIssue(ctx, projects.Owner, p.Slug, issueNo, map[string]any{
		"state": "closed",
	})
	if err != nil {
		return fmt.Errorf("queue: complete issue %d: %w", issueNo, err)
	}
	return nil
}

// List returns all issues in the given state ("open" or "closed").
// Caller-side helper; thin wrapper over ListIssues for symmetry.
func List(ctx context.Context, c *gitserver.Client, p *projects.Project, state string) ([]gitserver.Issue, error) {
	if state == "" {
		state = "open"
	}
	return c.ListIssues(ctx, projects.Owner, p.Slug, state, nil)
}

// HasLabel reports whether the issue carries the given label.
func HasLabel(i *gitserver.Issue, name string) bool {
	for _, l := range i.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// ClaimedBy returns the agent ID that claimed the issue, or "" if unclaimed.
func ClaimedBy(i *gitserver.Issue) string {
	for _, l := range i.Labels {
		if id, ok := strings.CutPrefix(l.Name, LabelClaimPrefix); ok {
			return id
		}
	}
	return ""
}

// Priority returns the priority label of the issue, defaulting to p2.
func Priority(i *gitserver.Issue) string {
	for _, l := range i.Labels {
		if validPriority(l.Name) {
			return l.Name
		}
	}
	return LabelP2
}

func filterClaimable(in []gitserver.Issue) []gitserver.Issue {
	out := in[:0:0]
	for _, i := range in {
		if HasLabel(&i, LabelInProgress) || ClaimedBy(&i) != "" {
			continue
		}
		out = append(out, i)
	}
	return out
}

func priorityRank(i *gitserver.Issue) int {
	switch Priority(i) {
	case LabelP1:
		return 1
	case LabelP2:
		return 2
	case LabelP3:
		return 3
	}
	return 9
}

func validPriority(s string) bool {
	switch s {
	case LabelP1, LabelP2, LabelP3:
		return true
	}
	return false
}

func labelNames(in []gitserver.Label) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		out = append(out, l.Name)
	}
	return out
}

// uniqueLabelIDs resolves label *names* into label IDs (the format
// Gitea's PATCH-issue endpoint requires for the "labels" field).
// Unknown labels (e.g. dynamic claimed:<agent-id> labels) are auto-created
// so PATCH calls don't silently drop them.
//
// The per-project label set is small (≤ a few dozen even with N agents);
// no caching needed for v1.
func uniqueLabelIDs(ctx context.Context, c *gitserver.Client, p *projects.Project, names []string) []int64 {
	if len(names) == 0 {
		return []int64{}
	}
	all, err := listLabels(ctx, c, p)
	if err != nil {
		return []int64{}
	}
	byName := make(map[string]int64, len(all))
	for _, l := range all {
		byName[l.Name] = l.ID
	}
	seen := make(map[int64]struct{}, len(names))
	out := make([]int64, 0, len(names))
	for _, n := range names {
		id, ok := byName[n]
		if !ok {
			created, err := c.CreateLabel(ctx, projects.Owner, p.Slug, n, dynamicLabelColor(n))
			if err != nil || created == nil {
				continue
			}
			id = created.ID
			byName[n] = id
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// dynamicLabelColor picks a stable-ish color for auto-created labels.
// Claim labels are grey; everything else falls back to grey.
func dynamicLabelColor(name string) string {
	if strings.HasPrefix(name, LabelClaimPrefix) {
		return "c0c0c0"
	}
	return "ededed"
}

func listLabels(ctx context.Context, c *gitserver.Client, p *projects.Project) ([]gitserver.Label, error) {
	return c.ListLabels(ctx, projects.Owner, p.Slug)
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "409")
}
