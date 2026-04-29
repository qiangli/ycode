package gitserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// GiteaMCPHandler implements mcp.ServerHandler, exposing the embedded Gitea
// git server to external AI agents via MCP protocol.
// Agents can manage repositories, branches, pull requests, and worktrees —
// enabling multi-agent collaboration through standard git workflows.
type GiteaMCPHandler struct {
	client *Client
}

// NewGiteaMCPHandler creates an MCP handler wrapping a Gitea API client.
func NewGiteaMCPHandler(client *Client) *GiteaMCPHandler {
	return &GiteaMCPHandler{client: client}
}

func (h *GiteaMCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		// Repository management
		{
			Name:        "list_repos",
			Description: "List all repositories on the git server. Returns repo names, clone URLs, and web UI links.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {}
			}`),
		},
		{
			Name:        "create_repo",
			Description: "Create a new repository on the git server. Use for agent collaboration, shared codebases, or code review workflows.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"name": {"type": "string", "description": "Repository name (lowercase, no spaces)"},
					"description": {"type": "string", "description": "Short description of the repository"}
				},
				"required": ["name"]
			}`),
		},
		// Branch management
		{
			Name:        "list_branches",
			Description: "List branches in a repository. Use to see agent branches and their latest commits.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"}
				},
				"required": ["owner", "repo"]
			}`),
		},
		{
			Name:        "create_branch",
			Description: "Create a new branch from an existing ref. Use to isolate agent work on a separate branch.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"branch": {"type": "string", "description": "New branch name"},
					"from": {"type": "string", "description": "Source branch or ref (default: main)"}
				},
				"required": ["owner", "repo", "branch"]
			}`),
		},
		// Pull request workflow
		{
			Name:        "create_pull_request",
			Description: "Create a pull request for code review. Use after an agent completes work on a branch to propose merging into the base branch.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"title": {"type": "string", "description": "Pull request title"},
					"head": {"type": "string", "description": "Source branch (agent's work branch)"},
					"base": {"type": "string", "description": "Target branch (e.g. main)"}
				},
				"required": ["owner", "repo", "title", "head", "base"]
			}`),
		},
		{
			Name:        "list_pull_requests",
			Description: "List pull requests in a repository. Filter by state (open, closed, all) to review agent work.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"state": {"type": "string", "enum": ["open", "closed", "all"], "description": "PR state filter (default: open)"}
				},
				"required": ["owner", "repo"]
			}`),
		},
		{
			Name:        "merge_pull_request",
			Description: "Merge a pull request. Integrates an agent's work branch into the base branch.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"number": {"type": "integer", "description": "Pull request number"},
					"method": {"type": "string", "enum": ["merge", "rebase", "squash"], "description": "Merge method (default: merge)"}
				},
				"required": ["owner", "repo", "number"]
			}`),
		},
		// Issue management
		{
			Name:        "create_issue",
			Description: "Create an issue in a repository. Use to track bugs, capability gaps, or improvement tasks.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"title": {"type": "string", "description": "Issue title"},
					"body": {"type": "string", "description": "Issue body (markdown)"},
					"labels": {"type": "array", "items": {"type": "string"}, "description": "Label names to apply"}
				},
				"required": ["owner", "repo", "title"]
			}`),
		},
		{
			Name:        "list_issues",
			Description: "List issues in a repository. Filter by state and labels to find capability gaps or tasks.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"state": {"type": "string", "enum": ["open", "closed", "all"], "description": "Issue state filter (default: open)"},
					"labels": {"type": "array", "items": {"type": "string"}, "description": "Filter by label names"}
				},
				"required": ["owner", "repo"]
			}`),
		},
		{
			Name:        "get_issue",
			Description: "Get a single issue by number, including its full body and labels.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"number": {"type": "integer", "description": "Issue number"}
				},
				"required": ["owner", "repo", "number"]
			}`),
		},
		{
			Name:        "update_issue",
			Description: "Update an issue's title, body, or state. Use to close resolved capability gaps.",
			InputSchema: mustMCPJSON(`{
				"type": "object",
				"properties": {
					"owner": {"type": "string", "description": "Repository owner"},
					"repo": {"type": "string", "description": "Repository name"},
					"number": {"type": "integer", "description": "Issue number"},
					"title": {"type": "string", "description": "New title (optional)"},
					"body": {"type": "string", "description": "New body (optional)"},
					"state": {"type": "string", "enum": ["open", "closed"], "description": "New state (optional)"}
				},
				"required": ["owner", "repo", "number"]
			}`),
		},
	}
}

func (h *GiteaMCPHandler) ListResources() []mcp.Resource {
	return nil
}

func (h *GiteaMCPHandler) ReadResource(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("no resources available")
}

func (h *GiteaMCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "list_repos":
		return h.handleListRepos(ctx)
	case "create_repo":
		return h.handleCreateRepo(ctx, input)
	case "list_branches":
		return h.handleListBranches(ctx, input)
	case "create_branch":
		return h.handleCreateBranch(ctx, input)
	case "create_pull_request":
		return h.handleCreatePR(ctx, input)
	case "list_pull_requests":
		return h.handleListPRs(ctx, input)
	case "merge_pull_request":
		return h.handleMergePR(ctx, input)
	case "create_issue":
		return h.handleCreateIssue(ctx, input)
	case "list_issues":
		return h.handleListIssues(ctx, input)
	case "get_issue":
		return h.handleGetIssue(ctx, input)
	case "update_issue":
		return h.handleUpdateIssue(ctx, input)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *GiteaMCPHandler) handleListRepos(ctx context.Context) (string, error) {
	repos, err := h.client.ListRepos(ctx)
	if err != nil {
		return "", err
	}
	if len(repos) == 0 {
		return "(no repositories)", nil
	}
	var lines []string
	for _, r := range repos {
		lines = append(lines, fmt.Sprintf("- %s  clone: %s  web: %s", r.FullName, r.CloneURL, r.HTMLURL))
	}
	return strings.Join(lines, "\n"), nil
}

func (h *GiteaMCPHandler) handleCreateRepo(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	repo, err := h.client.CreateRepo(ctx, p.Name, p.Description)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created %s\nClone: %s\nWeb: %s", repo.FullName, repo.CloneURL, repo.HTMLURL), nil
}

func (h *GiteaMCPHandler) handleListBranches(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	branches, err := h.client.ListBranches(ctx, p.Owner, p.Repo)
	if err != nil {
		return "", err
	}
	if len(branches) == 0 {
		return "(no branches)", nil
	}
	var lines []string
	for _, b := range branches {
		lines = append(lines, fmt.Sprintf("- %s  (%s: %s)", b.Name, b.Commit.ID[:8], b.Commit.Message))
	}
	return strings.Join(lines, "\n"), nil
}

func (h *GiteaMCPHandler) handleCreateBranch(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
		From   string `json:"from"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if p.From == "" {
		p.From = "main"
	}
	branch, err := h.client.CreateBranch(ctx, p.Owner, p.Repo, p.Branch, p.From)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created branch %s from %s (at %s)", branch.Name, p.From, branch.Commit.ID[:8]), nil
}

func (h *GiteaMCPHandler) handleCreatePR(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Title string `json:"title"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	pr, err := h.client.CreatePR(ctx, p.Owner, p.Repo, p.Title, p.Head, p.Base)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created PR #%d: %s\n%s → %s\nURL: %s", pr.Number, pr.Title, pr.Head.Ref, pr.Base.Ref, pr.HTMLURL), nil
}

func (h *GiteaMCPHandler) handleListPRs(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if p.State == "" {
		p.State = "open"
	}
	prs, err := h.client.ListPRs(ctx, p.Owner, p.Repo, p.State)
	if err != nil {
		return "", err
	}
	if len(prs) == 0 {
		return fmt.Sprintf("(no %s pull requests)", p.State), nil
	}
	var lines []string
	for _, pr := range prs {
		lines = append(lines, fmt.Sprintf("- #%d %s [%s] %s → %s  %s", pr.Number, pr.Title, pr.State, pr.Head.Ref, pr.Base.Ref, pr.HTMLURL))
	}
	return strings.Join(lines, "\n"), nil
}

func (h *GiteaMCPHandler) handleMergePR(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int64  `json:"number"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if err := h.client.MergePR(ctx, p.Owner, p.Repo, p.Number, p.Method); err != nil {
		return "", err
	}
	method := p.Method
	if method == "" {
		method = "merge"
	}
	return fmt.Sprintf("Merged PR #%d via %s", p.Number, method), nil
}

func (h *GiteaMCPHandler) handleCreateIssue(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner  string   `json:"owner"`
		Repo   string   `json:"repo"`
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	issue, err := h.client.CreateIssue(ctx, p.Owner, p.Repo, p.Title, p.Body, p.Labels)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created issue #%d: %s\nURL: %s", issue.Number, issue.Title, issue.HTMLURL), nil
}

func (h *GiteaMCPHandler) handleListIssues(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner  string   `json:"owner"`
		Repo   string   `json:"repo"`
		State  string   `json:"state"`
		Labels []string `json:"labels"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	if p.State == "" {
		p.State = "open"
	}
	issues, err := h.client.ListIssues(ctx, p.Owner, p.Repo, p.State, p.Labels)
	if err != nil {
		return "", err
	}
	if len(issues) == 0 {
		return fmt.Sprintf("(no %s issues)", p.State), nil
	}
	var lines []string
	for _, i := range issues {
		labelNames := make([]string, len(i.Labels))
		for j, l := range i.Labels {
			labelNames[j] = l.Name
		}
		labelStr := ""
		if len(labelNames) > 0 {
			labelStr = " [" + strings.Join(labelNames, ", ") + "]"
		}
		lines = append(lines, fmt.Sprintf("- #%d %s [%s]%s  %s", i.Number, i.Title, i.State, labelStr, i.HTMLURL))
	}
	return strings.Join(lines, "\n"), nil
}

func (h *GiteaMCPHandler) handleGetIssue(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int64  `json:"number"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	issue, err := h.client.GetIssue(ctx, p.Owner, p.Repo, p.Number)
	if err != nil {
		return "", err
	}
	labelNames := make([]string, len(issue.Labels))
	for j, l := range issue.Labels {
		labelNames[j] = l.Name
	}
	return fmt.Sprintf("#%d %s [%s]\nLabels: %s\nURL: %s\n\n%s",
		issue.Number, issue.Title, issue.State,
		strings.Join(labelNames, ", "),
		issue.HTMLURL, issue.Body), nil
}

func (h *GiteaMCPHandler) handleUpdateIssue(ctx context.Context, input json.RawMessage) (string, error) {
	var p struct {
		Owner  string `json:"owner"`
		Repo   string `json:"repo"`
		Number int64  `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return "", err
	}
	updates := map[string]any{}
	if p.Title != "" {
		updates["title"] = p.Title
	}
	if p.Body != "" {
		updates["body"] = p.Body
	}
	if p.State != "" {
		updates["state"] = p.State
	}
	issue, err := h.client.UpdateIssue(ctx, p.Owner, p.Repo, p.Number, updates)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Updated issue #%d: %s [%s]", issue.Number, issue.Title, issue.State), nil
}

// mustMCPJSON parses a JSON string or panics — for inline schema literals.
func mustMCPJSON(s string) json.RawMessage {
	var v json.RawMessage
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		panic(fmt.Sprintf("invalid JSON schema: %v", err))
	}
	return v
}
