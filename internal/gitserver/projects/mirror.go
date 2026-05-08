package projects

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// MirrorOptions controls how the cwd is mirrored to upstream.
type MirrorOptions struct {
	// CloneURL is the http(s) URL of the project's repo in internal Gitea
	// (Repository.CloneURL from the Gitea API). Required.
	CloneURL string
	// Token is the admin token used to authenticate the push. Required.
	Token string
	// Force allows non-fast-forward updates. Used when the user has rebased
	// cwd's history; agents see the new history afterwards.
	Force bool
}

// MirrorUpstream pushes the cwd's HEAD branch into the project's tracking
// repo on internal Gitea. The pushed branch is always named "main"
// regardless of what the local branch is called.
//
// This is a no-op when there is nothing to push.
func MirrorUpstream(ctx context.Context, cwd string, opts MirrorOptions) error {
	if opts.CloneURL == "" {
		return fmt.Errorf("mirror: empty CloneURL")
	}
	if opts.Token == "" {
		return fmt.Errorf("mirror: empty Token")
	}
	repo, err := git.PlainOpen(cwd)
	if err != nil {
		return fmt.Errorf("mirror: open %s: %w", cwd, err)
	}
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("mirror: read HEAD: %w", err)
	}
	if !head.Name().IsBranch() {
		return fmt.Errorf("mirror: HEAD is not a branch (%s); commit first", head.Name())
	}

	const remote = "ycode-internal"
	if err := ensureRemote(repo, remote, opts.CloneURL); err != nil {
		return err
	}

	refspec := fmt.Sprintf("+%s:refs/heads/main", head.Name())
	pushOpts := &git.PushOptions{
		RemoteName: remote,
		RefSpecs:   []config.RefSpec{config.RefSpec(refspec)},
		Force:      opts.Force,
		Auth: &http.BasicAuth{
			// Gitea accepts "token" as the username with the admin token as
			// the password for HTTP basic auth (its standard token-auth).
			Username: "token",
			Password: opts.Token,
		},
	}
	err = repo.PushContext(ctx, pushOpts)
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mirror: push to %s: %w", remote, err)
	}
	return nil
}

func ensureRemote(repo *git.Repository, name, url string) error {
	cfg, err := repo.Config()
	if err != nil {
		return fmt.Errorf("mirror: read config: %w", err)
	}
	if existing, ok := cfg.Remotes[name]; ok {
		if len(existing.URLs) > 0 && existing.URLs[0] == url {
			return nil
		}
		// Replace stale URL.
		_ = repo.DeleteRemote(name)
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return fmt.Errorf("mirror: add remote %s: %w", name, err)
	}
	return nil
}

// HeadSHA returns the cwd's current HEAD SHA. Helper for callers that want
// to compare against upstream's last-known SHA before deciding to mirror.
func HeadSHA(cwd string) (string, error) {
	repo, err := git.PlainOpen(cwd)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

// IsClean reports whether cwd has no uncommitted changes.
func IsClean(cwd string) (bool, error) {
	repo, err := git.PlainOpen(cwd)
	if err != nil {
		return false, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return false, err
	}
	st, err := wt.Status()
	if err != nil {
		return false, err
	}
	return st.IsClean(), nil
}

// HeadRef returns the symbolic ref name of HEAD (e.g. "refs/heads/main").
func HeadRef(cwd string) (plumbing.ReferenceName, error) {
	repo, err := git.PlainOpen(cwd)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Name(), nil
}
