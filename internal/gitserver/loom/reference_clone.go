package loom

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	loompkg "github.com/qiangli/ycode/pkg/loom"
)

// prepareReferenceCloneSandbox lays out a sandbox using
// `git clone --reference <bare> <gitea-http-url>` so the child's
// .git/objects shares the parent bare repo's object database via
// alternates while keeping per-clone refs / index / stash / reflog /
// hooks isolated.
//
// Output path: <sandboxRoot>/<agentID>/issue-0 (matching the existing
// collab convention so the rest of the lease pipeline doesn't need
// to know which path produced the sandbox).
//
// Token threads through the HTTP URL via the standard
// http://<token>@host/... shape so the push later succeeds without
// re-prompting for credentials.
func prepareReferenceCloneSandbox(
	ctx context.Context,
	sandboxRoot, giteaDataDir, slug, branch, agentID, cloneURL, token string,
) (string, error) {
	bare := loompkg.BareRepoPath(giteaDataDir, slug)
	if _, err := os.Stat(bare); err != nil {
		return "", fmt.Errorf("loom: reference-clone: bare repo missing at %s: %w", bare, err)
	}

	// Mimic collab's <agentID>/issue-0 layout so downstream code paths
	// (CommitAndPush, Release) see the same shape.
	parent := filepath.Join(sandboxRoot, agentID)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("loom: mkdir sandbox parent: %w", err)
	}
	target := filepath.Join(parent, "issue-0")
	// Clean up a stale path if one is sitting around (idempotent
	// re-lease for the same agent).
	if err := os.RemoveAll(target); err != nil {
		return "", fmt.Errorf("loom: clean stale sandbox: %w", err)
	}

	// Build the clone URL with token embedded for HTTP push auth.
	authedURL, err := injectToken(cloneURL, token)
	if err != nil {
		return "", err
	}

	// `git clone --reference <bare> <url> <target> -b <branch>` —
	// the --reference flag installs .git/objects/info/alternates
	// pointing at <bare>/objects. The -b flag checks out <branch> if
	// it exists; if not, we fall back to creating it post-clone.
	if err := runGit(ctx, parent, "clone", "--reference", bare, authedURL, target); err != nil {
		return "", fmt.Errorf("loom: reference-clone: %w", err)
	}

	// Establish the lease's branch — checkout if it already exists on
	// the remote (-B forces reset), or create from current HEAD.
	// `git checkout -B <branch>` is safe whether or not the branch
	// exists locally.
	if err := runGit(ctx, target, "checkout", "-B", branch); err != nil {
		return "", fmt.Errorf("loom: reference-clone checkout %s: %w", branch, err)
	}

	return target, nil
}

// injectToken wraps an http(s):// URL with the Gitea admin token so
// subsequent push operations succeed without re-prompting. Mirrors
// the credential-embed pattern collab.PrepareSandbox uses.
func injectToken(cloneURL, token string) (string, error) {
	if token == "" {
		return cloneURL, nil
	}
	// Cheap string-level injection — avoids importing net/url here.
	// Format: http://<token>@host/path — Gitea recognizes the token
	// as a basic-auth password with empty username.
	const httpsPrefix = "https://"
	const httpPrefix = "http://"
	switch {
	case len(cloneURL) > len(httpsPrefix) && cloneURL[:len(httpsPrefix)] == httpsPrefix:
		return httpsPrefix + token + "@" + cloneURL[len(httpsPrefix):], nil
	case len(cloneURL) > len(httpPrefix) && cloneURL[:len(httpPrefix)] == httpPrefix:
		return httpPrefix + token + "@" + cloneURL[len(httpPrefix):], nil
	default:
		return "", fmt.Errorf("loom: unsupported clone URL scheme: %s", cloneURL)
	}
}
