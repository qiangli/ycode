package collab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/gitserver/agents"
	"github.com/qiangli/ycode/internal/runtime/git"
)

// PrepareSandbox clones the project's tracking repo (admin/<slug>) into
// a per-agent sandbox dir, creates and checks out the agent branch,
// and configures the agent's git author identity so commits are
// attributed to agent-<id> regardless of the OS user running the
// process.
//
// Layout:
//
//	<sandboxRoot>/<agent-id>/<issue-N>/   — sandbox cwd
//
// Idempotent: if the sandbox already exists, it's removed and recreated
// (issues can be re-claimed after a previous agent abandoned).
func PrepareSandbox(ctx context.Context, sandboxRoot, cloneURL, token string, a *agents.Agent, issueNo int64, branch string) (string, error) {
	if sandboxRoot == "" || cloneURL == "" || token == "" {
		return "", fmt.Errorf("collab.PrepareSandbox: empty sandboxRoot, cloneURL, or token")
	}
	if a == nil || a.ID == "" {
		return "", fmt.Errorf("collab.PrepareSandbox: nil agent or empty ID")
	}
	dir := filepath.Join(sandboxRoot, a.ID, fmt.Sprintf("issue-%d", issueNo))
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("collab: clean sandbox: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("collab: mkdir parent: %w", err)
	}

	authURL := injectToken(cloneURL, token)

	ge := git.NewGitExec(nil)
	if _, err := ge.Run(ctx, filepath.Dir(dir), "clone", "--branch", "main", authURL, filepath.Base(dir)); err != nil {
		return "", fmt.Errorf("collab: git clone: %w", err)
	}
	if _, err := ge.Run(ctx, dir, "checkout", "-b", branch); err != nil {
		return "", fmt.Errorf("collab: checkout %s: %w", branch, err)
	}

	// Author identity on this clone — every commit is attributed to the
	// agent identity, not the OS user running ycode.
	parts := strings.SplitN(a.AuthorTrailer(), " <", 2)
	authorName := parts[0]
	authorEmail := strings.TrimSuffix(parts[1], ">")
	if _, err := ge.Run(ctx, dir, "config", "user.name", authorName); err != nil {
		return "", fmt.Errorf("collab: set user.name: %w", err)
	}
	if _, err := ge.Run(ctx, dir, "config", "user.email", authorEmail); err != nil {
		return "", fmt.Errorf("collab: set user.email: %w", err)
	}
	return dir, nil
}

// injectToken rewrites http(s) URLs to embed the auth token. Any
// non-http URL passes through unchanged so this is safe to call on
// anything we get back from Gitea.
func injectToken(rawURL, token string) string {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	scheme := "http://"
	rest := strings.TrimPrefix(rawURL, scheme)
	if rest == rawURL {
		scheme = "https://"
		rest = strings.TrimPrefix(rawURL, scheme)
	}
	return fmt.Sprintf("%stoken:%s@%s", scheme, token, rest)
}
