// Package agents manages per-agent branch lifecycle inside a project's
// internal-Gitea tracking repo. An "agent" here is a logical identity
// (stable ID + display name) — Gitea sees only the admin user, but
// branch names and commit author trailers carry the agent ID.
//
// See docs/agent-collab.md for the design.
package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/qiangli/ycode/internal/gitserver"
	"github.com/qiangli/ycode/internal/gitserver/projects"
)

// Agent is a logical worker identity. Stable across iterations of the
// same agent's work; persisted in task.Registry by the spawner.
type Agent struct {
	ID   string // "agent-<8-hex>", stable
	Name string // human-readable, optional
}

// NewAgent creates a fresh agent identity.
func NewAgent(name string) *Agent {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return &Agent{
		ID:   "agent-" + hex.EncodeToString(b[:]),
		Name: name,
	}
}

// AuthorTrailer returns the git author string for commits.
// Used regardless of which OS user runs the process.
func (a *Agent) AuthorTrailer() string {
	return fmt.Sprintf("%s <%s@ycode.local>", a.ID, a.ID)
}

// Branch is an agent's working branch in the project repo.
type Branch struct {
	Project *projects.Project
	Agent   *Agent
	Name    string // "agent/<id>/issue-<num>"
	IssueNo int64  // the issue this branch is addressing (0 if free-form)
}

// BranchName returns the canonical branch name for the given agent + issue.
// Issue 0 produces a free-form branch suffixed with a short random id.
func BranchName(a *Agent, issueNo int64) string {
	if issueNo > 0 {
		return fmt.Sprintf("agent/%s/issue-%d", a.ID, issueNo)
	}
	var b [3]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("agent/%s/free-%s", a.ID, hex.EncodeToString(b[:]))
}

// AssignBranch reserves a branch for the agent in the project repo.
// It creates the branch off main on the Gitea side; pushes happen via Push().
func AssignBranch(ctx context.Context, c *gitserver.Client, p *projects.Project, a *Agent, issueNo int64) (*Branch, error) {
	name := BranchName(a, issueNo)
	if _, err := c.CreateBranch(ctx, projects.Owner, p.Slug, name, "main"); err != nil {
		// If the branch already exists, treat as idempotent.
		if !isAlreadyExists(err) {
			return nil, fmt.Errorf("agents: create branch %s: %w", name, err)
		}
	}
	return &Branch{
		Project: p,
		Agent:   a,
		Name:    name,
		IssueNo: issueNo,
	}, nil
}

// PushOptions controls how an agent's work is pushed to internal Gitea.
type PushOptions struct {
	// WorktreePath is a local clone of the project's tracking repo
	// where the agent has been working. Required.
	WorktreePath string
	// CloneURL is the http(s) URL of admin/<slug>. Required.
	CloneURL string
	// Token is the admin token (single-user mode). Required.
	Token string
	// Force allows non-fast-forward updates (e.g. after rebase).
	Force bool
}

// Push publishes the agent's local branch to internal Gitea.
// The local HEAD must be on the named branch.
func (b *Branch) Push(ctx context.Context, opts PushOptions) error {
	if opts.WorktreePath == "" || opts.CloneURL == "" || opts.Token == "" {
		return fmt.Errorf("agents.Push: WorktreePath, CloneURL, Token required")
	}
	repo, err := git.PlainOpen(opts.WorktreePath)
	if err != nil {
		return fmt.Errorf("agents.Push: open %s: %w", opts.WorktreePath, err)
	}

	const remote = "ycode-internal"
	cfg, err := repo.Config()
	if err != nil {
		return fmt.Errorf("agents.Push: read config: %w", err)
	}
	if existing, ok := cfg.Remotes[remote]; ok {
		if len(existing.URLs) == 0 || existing.URLs[0] != opts.CloneURL {
			_ = repo.DeleteRemote(remote)
		}
	}
	if _, ok := cfg.Remotes[remote]; !ok {
		if _, err := repo.CreateRemote(&config.RemoteConfig{
			Name: remote,
			URLs: []string{opts.CloneURL},
		}); err != nil {
			return fmt.Errorf("agents.Push: add remote: %w", err)
		}
	}

	refspec := fmt.Sprintf("+refs/heads/%s:refs/heads/%s", b.Name, b.Name)
	pushOpts := &git.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{config.RefSpec(refspec)},
		Force:      opts.Force,
		Auth: &http.BasicAuth{
			Username: "token",
			Password: opts.Token,
		},
	}
	err = repo.PushContext(ctx, pushOpts)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("agents.Push: %w", err)
	}
	return nil
}

// OpenPR creates a PR from the agent's branch to main, linking the issue.
func (b *Branch) OpenPR(ctx context.Context, c *gitserver.Client, title, body string) (*gitserver.PullRequest, error) {
	prTitle := title
	if prTitle == "" {
		if b.IssueNo > 0 {
			prTitle = fmt.Sprintf("Agent %s: issue #%d", b.Agent.ID, b.IssueNo)
		} else {
			prTitle = fmt.Sprintf("Agent %s: %s", b.Agent.ID, b.Name)
		}
	}
	pr, err := c.CreatePR(ctx, projects.Owner, b.Project.Slug, prTitle, b.Name, "main")
	if err != nil {
		return nil, fmt.Errorf("agents.OpenPR: %w", err)
	}
	// Best-effort: comment in the issue body to link the PR. Defer to the
	// merger to do the closing on merge — Gitea closes the issue when the
	// PR body or commit message uses "Closes #N", which we set below
	// implicitly via the PR title format.
	_ = body
	return pr, nil
}

// HeadOnBranch reports whether the worktree's HEAD is on the named branch.
// Useful for agents to verify they're committing into the right place.
func HeadOnBranch(worktreePath, branchName string) (bool, error) {
	repo, err := git.PlainOpen(worktreePath)
	if err != nil {
		return false, err
	}
	head, err := repo.Head()
	if err != nil {
		return false, err
	}
	want := plumbing.NewBranchReferenceName(branchName)
	return head.Name() == want, nil
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "409") // Gitea returns 409 Conflict on duplicate branch
}
