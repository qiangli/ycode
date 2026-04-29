package github

import (
	"context"
	"fmt"
	"io"
	"strings"

	ghlib "github.com/google/go-github/v84/github"
)

// PR represents a pull request with flattened fields.
type PR struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	State     string `json:"state"`
	HeadRef   string `json:"head_ref"`
	BaseRef   string `json:"base_ref"`
	Author    string `json:"author"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Mergeable bool   `json:"mergeable"`
	Draft     bool   `json:"draft"`
}

// Issue represents a GitHub issue with flattened fields.
type Issue struct {
	Number    int      `json:"number"`
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	State     string   `json:"state"`
	Author    string   `json:"author"`
	Labels    []string `json:"labels"`
	URL       string   `json:"url"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// Comment represents a PR or issue comment.
type Comment struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// CheckRun represents a CI check status.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// Client wraps go-github with convenience methods for ycode.
type Client struct {
	gh    *ghlib.Client
	Owner string
	Repo  string
}

// NewGitHubClient creates a new GitHub client for the given owner/repo.
func NewGitHubClient(gh *ghlib.Client, owner, repo string) *Client {
	return &Client{gh: gh, Owner: owner, Repo: repo}
}

// CreatePR creates a new pull request.
func (c *Client) CreatePR(ctx context.Context, title, body, head, base string) (*PR, error) {
	pr, _, err := c.gh.PullRequests.Create(ctx, c.Owner, c.Repo, &ghlib.NewPullRequest{
		Title: ghlib.Ptr(title),
		Body:  ghlib.Ptr(body),
		Head:  ghlib.Ptr(head),
		Base:  ghlib.Ptr(base),
	})
	if err != nil {
		return nil, fmt.Errorf("create PR: %w", err)
	}
	return convertPR(pr), nil
}

// ListPRs lists pull requests with the given state filter.
func (c *Client) ListPRs(ctx context.Context, state string, limit int) ([]PR, error) {
	if limit <= 0 {
		limit = 10
	}
	prs, _, err := c.gh.PullRequests.List(ctx, c.Owner, c.Repo, &ghlib.PullRequestListOptions{
		State:       state,
		ListOptions: ghlib.ListOptions{PerPage: limit},
	})
	if err != nil {
		return nil, fmt.Errorf("list PRs: %w", err)
	}

	result := make([]PR, len(prs))
	for i, pr := range prs {
		result[i] = *convertPR(pr)
	}
	return result, nil
}

// GetPR gets a single pull request by number.
func (c *Client) GetPR(ctx context.Context, number int) (*PR, error) {
	pr, _, err := c.gh.PullRequests.Get(ctx, c.Owner, c.Repo, number)
	if err != nil {
		return nil, fmt.Errorf("get PR #%d: %w", number, err)
	}
	return convertPR(pr), nil
}

// GetPRDiff returns the diff for a pull request.
func (c *Client) GetPRDiff(ctx context.Context, number int) (string, error) {
	diff, _, err := c.gh.PullRequests.GetRaw(ctx, c.Owner, c.Repo, number, ghlib.RawOptions{
		Type: ghlib.Diff,
	})
	if err != nil {
		return "", fmt.Errorf("get PR #%d diff: %w", number, err)
	}
	return diff, nil
}

// ListPRComments lists review comments on a pull request.
func (c *Client) ListPRComments(ctx context.Context, number int) ([]Comment, error) {
	// Get issue comments (conversation comments).
	comments, _, err := c.gh.Issues.ListComments(ctx, c.Owner, c.Repo, number, &ghlib.IssueListCommentsOptions{
		ListOptions: ghlib.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list PR #%d comments: %w", number, err)
	}

	result := make([]Comment, len(comments))
	for i, c := range comments {
		result[i] = Comment{
			ID:        c.GetID(),
			Author:    c.GetUser().GetLogin(),
			Body:      c.GetBody(),
			CreatedAt: c.GetCreatedAt().Time.Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

// CreatePRReview submits a review on a pull request.
// event must be one of: "APPROVE", "REQUEST_CHANGES", "COMMENT".
func (c *Client) CreatePRReview(ctx context.Context, number int, event, body string) error {
	_, _, err := c.gh.PullRequests.CreateReview(ctx, c.Owner, c.Repo, number, &ghlib.PullRequestReviewRequest{
		Event: ghlib.Ptr(event),
		Body:  ghlib.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("create review on PR #%d: %w", number, err)
	}
	return nil
}

// ListIssues lists issues with filters.
func (c *Client) ListIssues(ctx context.Context, state string, labels []string, limit int) ([]Issue, error) {
	if limit <= 0 {
		limit = 10
	}
	issues, _, err := c.gh.Issues.ListByRepo(ctx, c.Owner, c.Repo, &ghlib.IssueListByRepoOptions{
		State:       state,
		Labels:      labels,
		ListOptions: ghlib.ListOptions{PerPage: limit},
	})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	var result []Issue
	for _, issue := range issues {
		// Skip pull requests (GitHub API returns them mixed with issues).
		if issue.PullRequestLinks != nil {
			continue
		}
		result = append(result, *convertIssue(issue))
	}
	return result, nil
}

// GetIssue gets a single issue by number, including comments.
func (c *Client) GetIssue(ctx context.Context, number int) (*Issue, error) {
	issue, _, err := c.gh.Issues.Get(ctx, c.Owner, c.Repo, number)
	if err != nil {
		return nil, fmt.Errorf("get issue #%d: %w", number, err)
	}
	return convertIssue(issue), nil
}

// GetIssueComments gets comments on an issue.
func (c *Client) GetIssueComments(ctx context.Context, number int) ([]Comment, error) {
	comments, _, err := c.gh.Issues.ListComments(ctx, c.Owner, c.Repo, number, &ghlib.IssueListCommentsOptions{
		ListOptions: ghlib.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("list issue #%d comments: %w", number, err)
	}

	result := make([]Comment, len(comments))
	for i, c := range comments {
		result[i] = Comment{
			ID:        c.GetID(),
			Author:    c.GetUser().GetLogin(),
			Body:      c.GetBody(),
			CreatedAt: c.GetCreatedAt().Time.Format("2006-01-02 15:04"),
		}
	}
	return result, nil
}

// CreateIssueComment adds a comment to an issue or PR.
func (c *Client) CreateIssueComment(ctx context.Context, number int, body string) error {
	_, _, err := c.gh.Issues.CreateComment(ctx, c.Owner, c.Repo, number, &ghlib.IssueComment{
		Body: ghlib.Ptr(body),
	})
	if err != nil {
		return fmt.Errorf("comment on issue #%d: %w", number, err)
	}
	return nil
}

// GetCheckRuns returns CI check statuses for a git ref.
func (c *Client) GetCheckRuns(ctx context.Context, ref string) ([]CheckRun, error) {
	checks, _, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.Owner, c.Repo, ref, &ghlib.ListCheckRunsOptions{
		ListOptions: ghlib.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, fmt.Errorf("get checks for %q: %w", ref, err)
	}

	result := make([]CheckRun, len(checks.CheckRuns))
	for i, cr := range checks.CheckRuns {
		result[i] = CheckRun{
			Name:       cr.GetName(),
			Status:     cr.GetStatus(),
			Conclusion: cr.GetConclusion(),
			URL:        cr.GetHTMLURL(),
		}
	}
	return result, nil
}

// GetPRFiles returns the list of files changed in a pull request.
func (c *Client) GetPRFiles(ctx context.Context, number int) ([]string, error) {
	files, _, err := c.gh.PullRequests.ListFiles(ctx, c.Owner, c.Repo, number, &ghlib.ListOptions{
		PerPage: 300,
	})
	if err != nil {
		return nil, fmt.Errorf("list PR #%d files: %w", number, err)
	}

	result := make([]string, len(files))
	for i, f := range files {
		status := f.GetStatus()
		result[i] = fmt.Sprintf("%s %s", status, f.GetFilename())
	}
	return result, nil
}

// FormatPR formats a PR for human-readable display.
func FormatPR(pr *PR) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d: %s\n", pr.Number, pr.Title)
	fmt.Fprintf(&b, "State: %s | Author: %s | Draft: %v\n", pr.State, pr.Author, pr.Draft)
	fmt.Fprintf(&b, "Branch: %s → %s\n", pr.HeadRef, pr.BaseRef)
	fmt.Fprintf(&b, "URL: %s\n", pr.URL)
	if pr.Body != "" {
		b.WriteString("\n")
		// Truncate very long bodies.
		body := pr.Body
		if len(body) > 2000 {
			body = body[:2000] + "\n... (truncated)"
		}
		b.WriteString(body)
	}
	return b.String()
}

// FormatIssue formats an issue for human-readable display.
func FormatIssue(issue *Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d: %s\n", issue.Number, issue.Title)
	fmt.Fprintf(&b, "State: %s | Author: %s\n", issue.State, issue.Author)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	fmt.Fprintf(&b, "URL: %s\n", issue.URL)
	if issue.Body != "" {
		b.WriteString("\n")
		body := issue.Body
		if len(body) > 2000 {
			body = body[:2000] + "\n... (truncated)"
		}
		b.WriteString(body)
	}
	return b.String()
}

// --- Conversion helpers ---

func convertPR(pr *ghlib.PullRequest) *PR {
	return &PR{
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		Body:      pr.GetBody(),
		State:     pr.GetState(),
		HeadRef:   pr.GetHead().GetRef(),
		BaseRef:   pr.GetBase().GetRef(),
		Author:    pr.GetUser().GetLogin(),
		URL:       pr.GetHTMLURL(),
		CreatedAt: pr.GetCreatedAt().Time.Format("2006-01-02 15:04"),
		UpdatedAt: pr.GetUpdatedAt().Time.Format("2006-01-02 15:04"),
		Mergeable: pr.GetMergeable(),
		Draft:     pr.GetDraft(),
	}
}

func convertIssue(issue *ghlib.Issue) *Issue {
	labels := make([]string, len(issue.Labels))
	for i, l := range issue.Labels {
		labels[i] = l.GetName()
	}
	return &Issue{
		Number:    issue.GetNumber(),
		Title:     issue.GetTitle(),
		Body:      issue.GetBody(),
		State:     issue.GetState(),
		Author:    issue.GetUser().GetLogin(),
		Labels:    labels,
		URL:       issue.GetHTMLURL(),
		CreatedAt: issue.GetCreatedAt().Time.Format("2006-01-02 15:04"),
		UpdatedAt: issue.GetUpdatedAt().Time.Format("2006-01-02 15:04"),
	}
}

// Ensure io import is used (for GetPRDiff raw response).
var _ = io.Discard
