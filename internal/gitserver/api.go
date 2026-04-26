package gitserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
