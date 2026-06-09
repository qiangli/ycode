package gitserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client provides typed access to the Gitea REST API for agent swarm operations.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates a Gitea API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Repository represents a Gitea repository.
type Repository struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	CloneURL string `json:"clone_url"`
	HTMLURL  string `json:"html_url"`
}

// Branch represents a git branch.
type Branch struct {
	Name   string `json:"name"`
	Commit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"commit"`
}

// PullRequest represents a Gitea pull request.
type PullRequest struct {
	ID     int64  `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Head   struct {
		Label string `json:"label"`
		Ref   string `json:"ref"`
	} `json:"head"`
	Base struct {
		Label string `json:"label"`
		Ref   string `json:"ref"`
	} `json:"base"`
	HTMLURL string `json:"html_url"`
}

// Issue represents a Gitea issue.
type Issue struct {
	ID      int64   `json:"id"`
	Number  int64   `json:"number"`
	Title   string  `json:"title"`
	Body    string  `json:"body"`
	State   string  `json:"state"`
	Labels  []Label `json:"labels"`
	HTMLURL string  `json:"html_url"`
}

// Label represents a Gitea label.
type Label struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// CreateRepo creates a new repository.
func (c *Client) CreateRepo(ctx context.Context, name, description string) (*Repository, error) {
	body := map[string]any{
		"name":           name,
		"description":    description,
		"auto_init":      true,
		"default_branch": "main",
		"private":        false,
	}
	var repo Repository
	if err := c.post(ctx, "/api/v1/user/repos", body, &repo); err != nil {
		return nil, err
	}
	return &repo, nil
}

// ListRepos lists repositories for the authenticated user.
func (c *Client) ListRepos(ctx context.Context) ([]Repository, error) {
	var repos []Repository
	if err := c.get(ctx, "/api/v1/user/repos", &repos); err != nil {
		return nil, err
	}
	return repos, nil
}

// CreateBranch creates a new branch from an existing ref.
func (c *Client) CreateBranch(ctx context.Context, owner, repo, branchName, fromRef string) (*Branch, error) {
	body := map[string]any{
		"new_branch_name": branchName,
		"old_branch_name": fromRef,
	}
	var branch Branch
	if err := c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/branches", owner, repo), body, &branch); err != nil {
		return nil, err
	}
	return &branch, nil
}

// ListBranches lists branches in a repository.
func (c *Client) ListBranches(ctx context.Context, owner, repo string) ([]Branch, error) {
	var branches []Branch
	if err := c.get(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/branches", owner, repo), &branches); err != nil {
		return nil, err
	}
	return branches, nil
}

// CreatePR creates a pull request.
func (c *Client) CreatePR(ctx context.Context, owner, repo, title, head, base string) (*PullRequest, error) {
	body := map[string]any{
		"title": title,
		"head":  head,
		"base":  base,
	}
	var pr PullRequest
	if err := c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/pulls", owner, repo), body, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// MergePR merges a pull request.
func (c *Client) MergePR(ctx context.Context, owner, repo string, prNumber int64, method string) error {
	if method == "" {
		method = "merge"
	}
	body := map[string]any{
		"Do": method,
	}
	return c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/merge", owner, repo, prNumber), body, nil)
}

// ListPRs lists pull requests in a repository.
func (c *Client) ListPRs(ctx context.Context, owner, repo, state string) ([]PullRequest, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/pulls?state=%s", owner, repo, state)
	var prs []PullRequest
	if err := c.get(ctx, path, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

// CreateIssue creates an issue in a repository.
func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body string, labels []string) (*Issue, error) {
	payload := map[string]any{
		"title": title,
		"body":  body,
	}
	if len(labels) > 0 {
		// Gitea accepts label IDs, but also supports label names via the
		// "labels" field when using the create issue endpoint with names.
		// We pass label names and let the caller resolve IDs if needed.
		payload["labels"] = labels
	}
	var issue Issue
	if err := c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/issues", owner, repo), payload, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// ListIssues lists issues in a repository, filtered by state and labels.
func (c *Client) ListIssues(ctx context.Context, owner, repo, state string, labels []string) ([]Issue, error) {
	path := fmt.Sprintf("/api/v1/repos/%s/%s/issues?type=issues", owner, repo)
	if state != "" {
		path += "&state=" + state
	}
	if len(labels) > 0 {
		path += "&labels=" + strings.Join(labels, ",")
	}
	var issues []Issue
	if err := c.get(ctx, path, &issues); err != nil {
		return nil, err
	}
	return issues, nil
}

// GetIssue gets a single issue by number.
func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int64) (*Issue, error) {
	var issue Issue
	if err := c.get(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d", owner, repo, number), &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// UpdateIssue updates an issue's fields (title, body, state).
func (c *Client) UpdateIssue(ctx context.Context, owner, repo string, number int64, updates map[string]any) (*Issue, error) {
	var issue Issue
	if err := c.patch(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d", owner, repo, number), updates, &issue); err != nil {
		return nil, err
	}
	return &issue, nil
}

// ListLabels lists labels in a repository.
func (c *Client) ListLabels(ctx context.Context, owner, repo string) ([]Label, error) {
	var labels []Label
	if err := c.get(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/labels", owner, repo), &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// CreateLabel creates a label in a repository.
func (c *Client) CreateLabel(ctx context.Context, owner, repo, name, color string) (*Label, error) {
	body := map[string]any{
		"name":  name,
		"color": "#" + color,
	}
	var label Label
	if err := c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/labels", owner, repo), body, &label); err != nil {
		return nil, err
	}
	return &label, nil
}

// AddIssueLabels appends labels to the given issue. Idempotent at
// Gitea's layer — adding a label the issue already has is a no-op.
// Returns the full label set after add.
func (c *Client) AddIssueLabels(ctx context.Context, owner, repo string, issueNumber int64, labelIDs []int64) ([]Label, error) {
	body := map[string]any{"labels": labelIDs}
	var labels []Label
	if err := c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/labels", owner, repo, issueNumber), body, &labels); err != nil {
		return nil, err
	}
	return labels, nil
}

// RemoveIssueLabel removes a single label from an issue. Best-effort:
// 404 (label not present) is not surfaced as an error.
func (c *Client) RemoveIssueLabel(ctx context.Context, owner, repo string, issueNumber, labelID int64) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/labels/%d", c.baseURL, owner, repo, issueNumber, labelID), nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitea remove issue label: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitea remove issue label: status %d: %s", resp.StatusCode, string(body))
	}
}

// ReplaceIssueLabels sets the labels on an issue to exactly the given
// set, removing any not in the list. Useful for state transitions
// where loom owns the entire `loom:*` namespace on an issue.
func (c *Client) ReplaceIssueLabels(ctx context.Context, owner, repo string, issueNumber int64, labelIDs []int64) ([]Label, error) {
	body := map[string]any{"labels": labelIDs}
	var labels []Label
	// PUT semantics for replace; underlying helper for PUT.
	req, err := http.NewRequestWithContext(ctx, "PUT",
		fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/%d/labels", c.baseURL, owner, repo, issueNumber), nil)
	if err != nil {
		return nil, err
	}
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea replace issue labels: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitea replace issue labels: status %d: %s", resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(resp.Body).Decode(&labels); err != nil {
		return nil, fmt.Errorf("gitea replace issue labels: decode: %w", err)
	}
	return labels, nil
}

// IssueComment is a Gitea issue comment.
type IssueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// CreateIssueComment posts a comment on an issue. Used by loom to drop
// the sticky-status comment that mirrors loom-process state inline on
// the issue page.
func (c *Client) CreateIssueComment(ctx context.Context, owner, repo string, issueNumber int64, body string) (*IssueComment, error) {
	var out IssueComment
	if err := c.post(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber),
		map[string]any{"body": body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EditIssueComment updates an existing comment. Used by loom to
// in-place-update the sticky-status comment rather than spamming the
// thread with a new comment per state transition.
func (c *Client) EditIssueComment(ctx context.Context, owner, repo string, commentID int64, body string) (*IssueComment, error) {
	req, err := http.NewRequestWithContext(ctx, "PATCH",
		fmt.Sprintf("%s/api/v1/repos/%s/%s/issues/comments/%d", c.baseURL, owner, repo, commentID), nil)
	if err != nil {
		return nil, err
	}
	bodyJSON, err := json.Marshal(map[string]any{"body": body})
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea edit issue comment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitea edit issue comment: status %d: %s", resp.StatusCode, string(body))
	}
	var out IssueComment
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("gitea edit issue comment: decode: %w", err)
	}
	return &out, nil
}

// ListIssueComments lists comments on an issue. Used by the sticky-
// comment manager to locate (or create) loom's owned comment by
// scanning the body for the sticky marker.
func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, issueNumber int64) ([]IssueComment, error) {
	var out []IssueComment
	if err := c.get(ctx, fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteBranch removes a branch from a repository. Best-effort:
// 404 (branch missing) is not surfaced as an error, mirroring how
// CreateBranch treats 409 (already exists).
func (c *Client) DeleteBranch(ctx context.Context, owner, repo, branch string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s", c.baseURL, owner, repo, branch), nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitea delete branch: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitea delete branch %d: %s", resp.StatusCode, string(body))
	}
}

// HTTP helpers.

func (c *Client) get(ctx context.Context, path string, result any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.doRequest(req, result)
}

func (c *Client) post(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req, result)
}

func (c *Client) patch(ctx context.Context, path string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "PATCH", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req, result)
}

func (c *Client) doRequest(req *http.Request, result any) error {
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gitea API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gitea API error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode gitea response: %w", err)
		}
	}

	return nil
}
