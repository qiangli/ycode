package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	gh "github.com/qiangli/ycode/internal/runtime/github"
)

// RegisterGitHubHandlers registers GitHub tool handlers (PR, issue, checks).
// All tools are deferred (discoverable via ToolSearch, not sent every request).
func RegisterGitHubHandlers(r *Registry, client *gh.Client) {
	r.Register(&ToolSpec{
		Name: "gh_pr_create",
		Description: "Create a GitHub pull request. " +
			"Requires title, head branch, and base branch.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"title": {"type": "string", "description": "PR title"},
				"body": {"type": "string", "description": "PR description (markdown)"},
				"head": {"type": "string", "description": "Head branch name"},
				"base": {"type": "string", "description": "Base branch name (e.g., main)"}
			},
			"required": ["title", "head", "base"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Title string `json:"title"`
				Body  string `json:"body"`
				Head  string `json:"head"`
				Base  string `json:"base"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			pr, err := client.CreatePR(ctx, params.Title, params.Body, params.Head, params.Base)
			if err != nil {
				return "", err
			}
			return gh.FormatPR(pr), nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_pr_list",
		Description: "List GitHub pull requests. " +
			"Filter by state (open, closed, all).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"state": {"type": "string", "description": "Filter: open, closed, or all", "default": "open"},
				"limit": {"type": "integer", "description": "Max results (default 10)"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				State string `json:"state"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.State == "" {
				params.State = "open"
			}
			prs, err := client.ListPRs(ctx, params.State, params.Limit)
			if err != nil {
				return "", err
			}
			if len(prs) == 0 {
				return fmt.Sprintf("No %s pull requests found.", params.State), nil
			}
			var sb strings.Builder
			for _, pr := range prs {
				fmt.Fprintf(&sb, "#%d %s (%s → %s) [%s] by %s\n",
					pr.Number, pr.Title, pr.HeadRef, pr.BaseRef, pr.State, pr.Author)
			}
			return sb.String(), nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_pr_get",
		Description: "Get details of a GitHub pull request, " +
			"including diff and changed files.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"number": {"type": "integer", "description": "PR number"},
				"include_diff": {"type": "boolean", "description": "Include the PR diff"}
			},
			"required": ["number"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Number      int  `json:"number"`
				IncludeDiff bool `json:"include_diff"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}

			pr, err := client.GetPR(ctx, params.Number)
			if err != nil {
				return "", err
			}
			result := gh.FormatPR(pr)

			if params.IncludeDiff {
				diff, err := client.GetPRDiff(ctx, params.Number)
				if err != nil {
					result += "\n\n(Failed to get diff: " + err.Error() + ")"
				} else {
					// Truncate very large diffs.
					if len(diff) > 10000 {
						diff = diff[:10000] + "\n... (diff truncated)"
					}
					result += "\n\n--- Diff ---\n" + diff
				}
			}

			return result, nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_pr_review",
		Description: "Submit a review on a GitHub pull request. " +
			"Event must be APPROVE, REQUEST_CHANGES, or COMMENT.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"number": {"type": "integer", "description": "PR number"},
				"event": {"type": "string", "description": "Review event: APPROVE, REQUEST_CHANGES, or COMMENT"},
				"body": {"type": "string", "description": "Review comment body"}
			},
			"required": ["number", "event", "body"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Number int    `json:"number"`
				Event  string `json:"event"`
				Body   string `json:"body"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			err := client.CreatePRReview(ctx, params.Number, params.Event, params.Body)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Review submitted on PR #%d: %s", params.Number, params.Event), nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_pr_comment",
		Description: "Add a comment to a GitHub pull request.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"number": {"type": "integer", "description": "PR number"},
				"body": {"type": "string", "description": "Comment body (markdown)"}
			},
			"required": ["number", "body"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Number int    `json:"number"`
				Body   string `json:"body"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			err := client.CreateIssueComment(ctx, params.Number, params.Body)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Comment added to PR #%d.", params.Number), nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_issue_list",
		Description: "List GitHub issues with optional state and label filters.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"state": {"type": "string", "description": "Filter: open, closed, or all", "default": "open"},
				"labels": {"type": "array", "items": {"type": "string"}, "description": "Filter by labels"},
				"limit": {"type": "integer", "description": "Max results (default 10)"}
			}
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				State  string   `json:"state"`
				Labels []string `json:"labels"`
				Limit  int      `json:"limit"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			if params.State == "" {
				params.State = "open"
			}
			issues, err := client.ListIssues(ctx, params.State, params.Labels, params.Limit)
			if err != nil {
				return "", err
			}
			if len(issues) == 0 {
				return fmt.Sprintf("No %s issues found.", params.State), nil
			}
			var sb strings.Builder
			for _, issue := range issues {
				labels := ""
				if len(issue.Labels) > 0 {
					labels = " [" + strings.Join(issue.Labels, ", ") + "]"
				}
				fmt.Fprintf(&sb, "#%d %s (%s)%s by %s\n",
					issue.Number, issue.Title, issue.State, labels, issue.Author)
			}
			return sb.String(), nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_issue_get",
		Description: "Get details of a GitHub issue including comments.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"number": {"type": "integer", "description": "Issue number"}
			},
			"required": ["number"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Number int `json:"number"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			issue, err := client.GetIssue(ctx, params.Number)
			if err != nil {
				return "", err
			}
			result := gh.FormatIssue(issue)

			comments, err := client.GetIssueComments(ctx, params.Number)
			if err == nil && len(comments) > 0 {
				result += "\n\n--- Comments ---\n"
				for _, c := range comments {
					result += fmt.Sprintf("\n%s (%s):\n%s\n", c.Author, c.CreatedAt, c.Body)
				}
			}

			return result, nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_issue_comment",
		Description: "Add a comment to a GitHub issue.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"number": {"type": "integer", "description": "Issue number"},
				"body": {"type": "string", "description": "Comment body (markdown)"}
			},
			"required": ["number", "body"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Number int    `json:"number"`
				Body   string `json:"body"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			err := client.CreateIssueComment(ctx, params.Number, params.Body)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Comment added to issue #%d.", params.Number), nil
		},
	})

	r.Register(&ToolSpec{
		Name: "gh_checks",
		Description: "Get CI check status for a git ref (branch, tag, or SHA).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"ref": {"type": "string", "description": "Git ref (branch name, tag, or commit SHA)"}
			},
			"required": ["ref"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Ref string `json:"ref"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse input: %w", err)
			}
			checks, err := client.GetCheckRuns(ctx, params.Ref)
			if err != nil {
				return "", err
			}
			if len(checks) == 0 {
				return fmt.Sprintf("No checks found for ref %q.", params.Ref), nil
			}
			var sb strings.Builder
			for _, check := range checks {
				conclusion := check.Conclusion
				if conclusion == "" {
					conclusion = check.Status // in_progress, queued, etc.
				}
				fmt.Fprintf(&sb, "%-30s %s\n", check.Name, conclusion)
			}
			return sb.String(), nil
		},
	})
}
