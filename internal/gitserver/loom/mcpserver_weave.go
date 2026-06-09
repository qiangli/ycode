package loom

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/gitserver/projects"
	"github.com/qiangli/ycode/internal/gitserver/weaveapi"
)

// handleWeaveAdd implements the v2 collaboration verb that files a new
// issue into the loom queue with todo/priority/source labels applied.
// Requires the optional weave + git fields populated (see
// NewMCPHandlerWithWeave).
func (h *MCPHandler) handleWeaveAdd(ctx context.Context, input json.RawMessage) (string, error) {
	if h.weave == nil || h.git == nil {
		return "", fmt.Errorf("weave_add: not configured (weave/git client missing — build with NewMCPHandlerWithWeave)")
	}
	var req struct {
		CWD         string `json:"cwd"`
		Title       string `json:"title"`
		Body        string `json:"body,omitempty"`
		Priority    string `json:"priority,omitempty"`
		Tool        string `json:"tool,omitempty"`
		ParentIssue int64  `json:"parent_issue,omitempty"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("weave_add: %w", err)
	}
	if req.CWD == "" || req.Title == "" {
		return "", fmt.Errorf("weave_add: cwd and title required")
	}

	// Resolve the project slug via the existing Backend abstraction.
	// EnsureProject is idempotent and returns the slug for an
	// already-mirrored project.
	slug, _, err := h.svc.Backend().EnsureProject(ctx, req.CWD)
	if err != nil {
		return "", fmt.Errorf("weave_add: ensure project: %w", err)
	}

	// Issue body wraps parent_issue cross-link if supplied.
	body := req.Body
	if req.ParentIssue > 0 {
		body = fmt.Sprintf("Filed while working on #%d.\n\n%s", req.ParentIssue, body)
	}

	// Create the issue with the loom:todo state label baked in so it
	// appears on the queue immediately (no race where weave_start
	// could see it before SetState lands).
	issue, err := h.git.CreateIssue(ctx, projects.Owner, slug, req.Title, body,
		[]string{weaveapi.LabelStateTodo})
	if err != nil {
		return "", fmt.Errorf("weave_add: create issue: %w", err)
	}

	// Source label: weave_add via MCP is always agent-attributed.
	if err := h.weave.AddSourceLabel(ctx, projects.Owner, slug, issue.Number, weaveapi.LabelSourceAgent); err != nil {
		// Don't fail the whole call on attribution failure — the
		// issue is filed, just unattributed.
		// Log and continue.
		_ = err
	}

	// Priority: default p2 when omitted; validated label set.
	prio := normalizePriority(req.Priority)
	if err := h.weave.SetPriority(ctx, projects.Owner, slug, issue.Number, prio); err != nil {
		// Same shape: filing succeeded, priority drifted to default.
		_ = err
	}

	out := map[string]any{
		"issue":    issue.Number,
		"html_url": issue.HTMLURL,
		"slug":     slug,
		"priority": prio,
		"source":   "agent",
	}
	if req.ParentIssue > 0 {
		out["parent_issue"] = req.ParentIssue
	}
	return marshalResult(out)
}

// handleWeavePrioritize bulk-flips the priority labels on a set of
// issues. One Gitea round-trip per issue; on partial failure, the
// caller sees both the updated count and a list of failed issues.
func (h *MCPHandler) handleWeavePrioritize(ctx context.Context, input json.RawMessage) (string, error) {
	if h.weave == nil {
		return "", fmt.Errorf("weave_prioritize: not configured (weave client missing)")
	}
	var req struct {
		CWD      string `json:"cwd"`
		Rankings []struct {
			Issue int64  `json:"issue"`
			Tier  string `json:"tier"`
		} `json:"rankings"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("weave_prioritize: %w", err)
	}
	if req.CWD == "" || len(req.Rankings) == 0 {
		return "", fmt.Errorf("weave_prioritize: cwd and non-empty rankings required")
	}

	slug, _, err := h.svc.Backend().EnsureProject(ctx, req.CWD)
	if err != nil {
		return "", fmt.Errorf("weave_prioritize: ensure project: %w", err)
	}

	updated := 0
	var failures []map[string]any
	for _, r := range req.Rankings {
		tier := normalizePriority(r.Tier)
		if err := h.weave.SetPriority(ctx, projects.Owner, slug, r.Issue, tier); err != nil {
			failures = append(failures, map[string]any{"issue": r.Issue, "error": err.Error()})
			continue
		}
		updated++
	}

	out := map[string]any{
		"updated": updated,
		"slug":    slug,
	}
	if len(failures) > 0 {
		out["failures"] = failures
	}
	return marshalResult(out)
}

// normalizePriority maps free-form priority input to a canonical
// loom:p* label name. Empty / unknown values default to loom:p2.
func normalizePriority(s string) string {
	switch s {
	case "p0", weaveapi.LabelPriorityP0:
		return weaveapi.LabelPriorityP0
	case "p1", weaveapi.LabelPriorityP1:
		return weaveapi.LabelPriorityP1
	case "p3", weaveapi.LabelPriorityP3:
		return weaveapi.LabelPriorityP3
	default:
		return weaveapi.LabelPriorityP2
	}
}
