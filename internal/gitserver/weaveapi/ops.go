package weaveapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/gitserver"
)

// Client wraps a gitserver.Client with v2 Loom-specific operations:
// label-namespace management, state transitions, priority updates,
// sticky-comment maintenance. Stateless beyond the wrapped client +
// label cache.
type Client struct {
	g     *gitserver.Client
	cache *labelCache
}

// NewClient returns a weaveapi.Client backed by the given Gitea client.
func NewClient(g *gitserver.Client) *Client {
	return &Client{g: g, cache: newLabelCache()}
}

// EnsureLabels creates any of the loom-owned labels (state, priority,
// source) that are missing from the repo. Idempotent: existing labels
// are left untouched. Returns the count of newly-created labels.
//
// Called by first-run setup; safe to call again at any time as a
// recovery operation if the label set was edited externally.
func (c *Client) EnsureLabels(ctx context.Context, owner, repo string) (int, error) {
	existing, err := c.g.ListLabels(ctx, owner, repo)
	if err != nil {
		return 0, fmt.Errorf("weaveapi: list labels: %w", err)
	}
	have := map[string]bool{}
	for _, l := range existing {
		have[l.Name] = true
		c.cache.put(owner, repo, l.Name, l.ID)
	}

	created := 0
	for _, spec := range AllLabelSpecs() {
		if have[spec.Name] {
			continue
		}
		l, err := c.g.CreateLabel(ctx, owner, repo, spec.Name, spec.Color)
		if err != nil {
			return created, fmt.Errorf("weaveapi: create label %q: %w", spec.Name, err)
		}
		c.cache.put(owner, repo, l.Name, l.ID)
		created++
	}
	return created, nil
}

// SetState transitions an issue's state label, removing any other
// state label currently on the issue. Single source of truth: loom
// owns the loom:<state> namespace on every managed issue.
func (c *Client) SetState(ctx context.Context, owner, repo string, issueNumber int64, newState string) error {
	if !IsStateLabel(newState) {
		return fmt.Errorf("weaveapi: SetState: %q is not a known loom state label", newState)
	}
	issue, err := c.g.GetIssue(ctx, owner, repo, issueNumber)
	if err != nil {
		return fmt.Errorf("weaveapi: get issue %d: %w", issueNumber, err)
	}

	// Remove any existing loom state labels other than the target.
	for _, l := range issue.Labels {
		if l.Name == newState || !IsStateLabel(l.Name) {
			continue
		}
		if err := c.g.RemoveIssueLabel(ctx, owner, repo, issueNumber, l.ID); err != nil {
			return fmt.Errorf("weaveapi: remove old state %q: %w", l.Name, err)
		}
	}

	// Add the target if not already present.
	for _, l := range issue.Labels {
		if l.Name == newState {
			return nil
		}
	}
	id, err := c.labelID(ctx, owner, repo, newState)
	if err != nil {
		return err
	}
	if _, err := c.g.AddIssueLabels(ctx, owner, repo, issueNumber, []int64{id}); err != nil {
		return fmt.Errorf("weaveapi: add state %q: %w", newState, err)
	}
	return nil
}

// SetPriority transitions an issue's priority-tier label, removing
// the prior tier label if any.
func (c *Client) SetPriority(ctx context.Context, owner, repo string, issueNumber int64, tier string) error {
	if !IsPriorityLabel(tier) {
		return fmt.Errorf("weaveapi: SetPriority: %q is not a loom priority label", tier)
	}
	issue, err := c.g.GetIssue(ctx, owner, repo, issueNumber)
	if err != nil {
		return fmt.Errorf("weaveapi: get issue %d: %w", issueNumber, err)
	}
	for _, l := range issue.Labels {
		if l.Name == tier || !IsPriorityLabel(l.Name) {
			continue
		}
		if err := c.g.RemoveIssueLabel(ctx, owner, repo, issueNumber, l.ID); err != nil {
			return fmt.Errorf("weaveapi: remove old priority %q: %w", l.Name, err)
		}
	}
	for _, l := range issue.Labels {
		if l.Name == tier {
			return nil
		}
	}
	id, err := c.labelID(ctx, owner, repo, tier)
	if err != nil {
		return err
	}
	if _, err := c.g.AddIssueLabels(ctx, owner, repo, issueNumber, []int64{id}); err != nil {
		return fmt.Errorf("weaveapi: add priority %q: %w", tier, err)
	}
	return nil
}

// AddSourceLabel applies the given source-attribution label without
// disturbing any existing source label. Used at issue-create time to
// stamp loom:source:human or loom:source:agent.
func (c *Client) AddSourceLabel(ctx context.Context, owner, repo string, issueNumber int64, source string) error {
	if source != LabelSourceHuman && source != LabelSourceAgent {
		return fmt.Errorf("weaveapi: AddSourceLabel: %q is not a loom source label", source)
	}
	id, err := c.labelID(ctx, owner, repo, source)
	if err != nil {
		return err
	}
	if _, err := c.g.AddIssueLabels(ctx, owner, repo, issueNumber, []int64{id}); err != nil {
		return fmt.Errorf("weaveapi: add source %q: %w", source, err)
	}
	return nil
}

const stickyCommentMarker = "<!-- loom:sticky -->"

// SetStickyComment ensures the issue has exactly one loom-owned
// sticky comment, creating or editing it as needed. The body is
// wrapped with stickyCommentMarker so we can find our own comment
// across restarts without storing state.
//
// Returns the comment ID for downstream callers that want to update
// it again without re-scanning the thread.
func (c *Client) SetStickyComment(ctx context.Context, owner, repo string, issueNumber int64, body string) (int64, error) {
	wrapped := stickyCommentMarker + "\n" + body
	comments, err := c.g.ListIssueComments(ctx, owner, repo, issueNumber)
	if err != nil {
		return 0, fmt.Errorf("weaveapi: list comments: %w", err)
	}
	for _, c2 := range comments {
		if strings.HasPrefix(c2.Body, stickyCommentMarker) {
			updated, err := c.g.EditIssueComment(ctx, owner, repo, c2.ID, wrapped)
			if err != nil {
				return 0, fmt.Errorf("weaveapi: edit sticky comment: %w", err)
			}
			return updated.ID, nil
		}
	}
	created, err := c.g.CreateIssueComment(ctx, owner, repo, issueNumber, wrapped)
	if err != nil {
		return 0, fmt.Errorf("weaveapi: create sticky comment: %w", err)
	}
	return created.ID, nil
}

// labelID looks up a label's ID in the cache, falling back to a fresh
// ListLabels call when missing. Lazy population so callers don't have
// to pre-prime.
func (c *Client) labelID(ctx context.Context, owner, repo, name string) (int64, error) {
	if id, ok := c.cache.get(owner, repo, name); ok {
		return id, nil
	}
	labels, err := c.g.ListLabels(ctx, owner, repo)
	if err != nil {
		return 0, fmt.Errorf("weaveapi: list labels: %w", err)
	}
	for _, l := range labels {
		c.cache.put(owner, repo, l.Name, l.ID)
	}
	if id, ok := c.cache.get(owner, repo, name); ok {
		return id, nil
	}
	return 0, fmt.Errorf("weaveapi: label %q not found (call EnsureLabels first)", name)
}
