package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler exposes ycode's GitHub integration to external coding
// agents over MCP. Wraps the go-github-backed Client so foreign agents
// don't need to shell out to `gh` or carry their own GitHub client.
//
// Auth resolution order (handled by NewClient): GITHUB_TOKEN env →
// GH_TOKEN env → ~/.config/gh/hosts.yml. Repo coordinates are detected
// from the `origin` remote of the project at the given `cwd`, unless
// the caller passes explicit `owner` / `repo` arguments.
//
// Permission tiers per tool:
//   - list/get/format calls: ReadOnly
//   - create_pr_review, create_issue_comment, create_pr: WorkspaceWrite
//     (they mutate remote state but not the local checkout).
//
// We deliberately do not expose merge / close / force-push variants —
// those are higher-impact and belong to `loom_merge` (workspace
// substrate) or an explicit human in the loop.
type MCPHandler struct{}

// NewMCPHandler builds the handler. Stateless — each call re-resolves
// auth and repo coordinates so multiple projects can share one
// `ycode mcp serve` process.
func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "github_list_prs",
			Description: "List pull requests for a GitHub repo. State is one of open|closed|all (default open). " +
				"owner/repo default to the origin remote of cwd.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":   {"type": "string"},
					"owner": {"type": "string"},
					"repo":  {"type": "string"},
					"state": {"type": "string", "enum": ["open", "closed", "all"]},
					"limit": {"type": "integer", "description": "Max PRs to return. Default 30."}
				}
			}`),
		},
		{
			Name:        "github_get_pr",
			Description: "Fetch a single pull request by number, including title, body, head/base, author, state, and labels.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"number": {"type": "integer"}
				},
				"required": ["number"]
			}`),
		},
		{
			Name:        "github_get_pr_diff",
			Description: "Fetch the unified diff text for a pull request.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"number": {"type": "integer"}
				},
				"required": ["number"]
			}`),
		},
		{
			Name:        "github_list_pr_comments",
			Description: "List review comments + issue-style comments for a pull request.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"number": {"type": "integer"}
				},
				"required": ["number"]
			}`),
		},
		{
			Name: "github_list_issues",
			Description: "List issues for a repo. State is one of open|closed|all (default open). " +
				"Labels, if non-empty, filters to issues bearing all listed labels.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"state":  {"type": "string", "enum": ["open", "closed", "all"]},
					"labels": {"type": "array", "items": {"type": "string"}},
					"limit":  {"type": "integer"}
				}
			}`),
		},
		{
			Name:        "github_get_issue",
			Description: "Fetch a single issue by number.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"number": {"type": "integer"}
				},
				"required": ["number"]
			}`),
		},
		{
			Name:        "github_get_check_runs",
			Description: "Fetch CI check-run status for a git ref (commit SHA, branch name, or tag).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":   {"type": "string"},
					"owner": {"type": "string"},
					"repo":  {"type": "string"},
					"ref":   {"type": "string"}
				},
				"required": ["ref"]
			}`),
		},
		{
			Name: "github_create_pr",
			Description: "Open a new pull request. Mutates remote state — requires WorkspaceWrite. " +
				"`head` is the source branch, `base` the target (typically main).",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":   {"type": "string"},
					"owner": {"type": "string"},
					"repo":  {"type": "string"},
					"title": {"type": "string"},
					"body":  {"type": "string"},
					"head":  {"type": "string"},
					"base":  {"type": "string"}
				},
				"required": ["title", "head", "base"]
			}`),
		},
		{
			Name:        "github_create_pr_review",
			Description: "Submit a pull request review. `event` is APPROVE | REQUEST_CHANGES | COMMENT.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"number": {"type": "integer"},
					"event":  {"type": "string", "enum": ["APPROVE", "REQUEST_CHANGES", "COMMENT"]},
					"body":   {"type": "string"}
				},
				"required": ["number", "event"]
			}`),
		},
		{
			Name:        "github_create_issue_comment",
			Description: "Post a comment on an issue or pull request.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"cwd":    {"type": "string"},
					"owner":  {"type": "string"},
					"repo":   {"type": "string"},
					"number": {"type": "integer"},
					"body":   {"type": "string"}
				},
				"required": ["number", "body"]
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode classifies each tool. Reads are ReadOnly; create_* tools
// mutate remote state and require WorkspaceWrite. None reach
// DangerFullAccess — even create_pr only mutates GitHub, not the host
// shell or the local filesystem.
func (h *MCPHandler) RequiredMode(toolName string) mcp.PermissionMode {
	switch toolName {
	case "github_create_pr", "github_create_pr_review", "github_create_issue_comment":
		return mcp.ModeWorkspaceWrite
	default:
		return mcp.ModeReadOnly
	}
}

// resolveClient builds a GitHub client and resolves (owner, repo) from
// the call args or by auto-detecting from the cwd's origin remote. The
// auth-missing case returns a specific error so handlers can produce
// useful messages (vs. a confusing nil deref from the underlying lib).
func resolveClient(ctx context.Context, cwd, ownerArg, repoArg string) (*Client, error) {
	gh := NewClient(ctx)
	if gh == nil {
		return nil, fmt.Errorf("github auth missing — set GITHUB_TOKEN, GH_TOKEN, or run `gh auth login`")
	}
	owner, repo := ownerArg, repoArg
	if owner == "" || repo == "" {
		dir := cwd
		if dir == "" {
			var err error
			dir, err = os.Getwd()
			if err != nil {
				return nil, fmt.Errorf("getwd: %w", err)
			}
		}
		o, r, err := DetectRepo(dir)
		if err != nil {
			return nil, fmt.Errorf("auto-detect owner/repo failed (pass them explicitly): %w", err)
		}
		if owner == "" {
			owner = o
		}
		if repo == "" {
			repo = r
		}
	}
	return NewGitHubClient(gh, owner, repo), nil
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	// Common arg shape — every tool accepts cwd/owner/repo.
	var common struct {
		Cwd   string `json:"cwd"`
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	}
	_ = json.Unmarshal(input, &common)

	switch name {
	case "github_list_prs":
		return h.listPRs(ctx, input, common)
	case "github_get_pr":
		return h.getPR(ctx, input, common)
	case "github_get_pr_diff":
		return h.getPRDiff(ctx, input, common)
	case "github_list_pr_comments":
		return h.listPRComments(ctx, input, common)
	case "github_list_issues":
		return h.listIssues(ctx, input, common)
	case "github_get_issue":
		return h.getIssue(ctx, input, common)
	case "github_get_check_runs":
		return h.getCheckRuns(ctx, input, common)
	case "github_create_pr":
		return h.createPR(ctx, input, common)
	case "github_create_pr_review":
		return h.createPRReview(ctx, input, common)
	case "github_create_issue_comment":
		return h.createIssueComment(ctx, input, common)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}

type commonArgs struct {
	Cwd   string `json:"cwd"`
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

func (h *MCPHandler) listPRs(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		State string `json:"state"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	state := args.State
	if state == "" {
		state = "open"
	}
	limit := args.Limit
	if limit == 0 {
		limit = 30
	}
	prs, err := cli.ListPRs(ctx, state, limit)
	if err != nil {
		return "", err
	}
	return marshalJSON(prs)
}

func (h *MCPHandler) getPR(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Number int `json:"number"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Number == 0 {
		return "", fmt.Errorf("number is required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	pr, err := cli.GetPR(ctx, args.Number)
	if err != nil {
		return "", err
	}
	return marshalJSON(pr)
}

func (h *MCPHandler) getPRDiff(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Number int `json:"number"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Number == 0 {
		return "", fmt.Errorf("number is required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	return cli.GetPRDiff(ctx, args.Number)
}

func (h *MCPHandler) listPRComments(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Number int `json:"number"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Number == 0 {
		return "", fmt.Errorf("number is required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	comments, err := cli.ListPRComments(ctx, args.Number)
	if err != nil {
		return "", err
	}
	return marshalJSON(comments)
}

func (h *MCPHandler) listIssues(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		State  string   `json:"state"`
		Labels []string `json:"labels"`
		Limit  int      `json:"limit"`
	}
	_ = json.Unmarshal(input, &args)
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	state := args.State
	if state == "" {
		state = "open"
	}
	limit := args.Limit
	if limit == 0 {
		limit = 30
	}
	issues, err := cli.ListIssues(ctx, state, args.Labels, limit)
	if err != nil {
		return "", err
	}
	return marshalJSON(issues)
}

func (h *MCPHandler) getIssue(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Number int `json:"number"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Number == 0 {
		return "", fmt.Errorf("number is required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	issue, err := cli.GetIssue(ctx, args.Number)
	if err != nil {
		return "", err
	}
	return marshalJSON(issue)
}

func (h *MCPHandler) getCheckRuns(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	runs, err := cli.GetCheckRuns(ctx, args.Ref)
	if err != nil {
		return "", err
	}
	return marshalJSON(runs)
}

func (h *MCPHandler) createPR(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Title string `json:"title"`
		Body  string `json:"body"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Title == "" || args.Head == "" || args.Base == "" {
		return "", fmt.Errorf("title, head, and base are required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	pr, err := cli.CreatePR(ctx, args.Title, args.Body, args.Head, args.Base)
	if err != nil {
		return "", err
	}
	return marshalJSON(pr)
}

func (h *MCPHandler) createPRReview(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Number int    `json:"number"`
		Event  string `json:"event"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Number == 0 || args.Event == "" {
		return "", fmt.Errorf("number and event are required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	if err := cli.CreatePRReview(ctx, args.Number, args.Event, args.Body); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

func (h *MCPHandler) createIssueComment(ctx context.Context, input json.RawMessage, c commonArgs) (string, error) {
	var args struct {
		commonArgs
		Number int    `json:"number"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", err
	}
	if args.Number == 0 || args.Body == "" {
		return "", fmt.Errorf("number and body are required")
	}
	cli, err := resolveClient(ctx, c.Cwd, c.Owner, c.Repo)
	if err != nil {
		return "", err
	}
	if err := cli.CreateIssueComment(ctx, args.Number, args.Body); err != nil {
		return "", err
	}
	return `{"ok":true}`, nil
}

func marshalJSON(v any) (string, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return string(out), nil
}
