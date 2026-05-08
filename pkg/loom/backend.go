package loom

import "context"

// Backend abstracts the underlying git/forge operations Service performs.
// The default gitea-backed implementation lives in
// internal/gitserver/loom; tests and alternative deployments can supply
// their own.
//
// All methods take ctx and return errors verbatim; Service does not wrap.
type Backend interface {
	// EnsureProject resolves cwd to a slug+cloneURL in the underlying
	// forge, creating the repo if it does not exist. Idempotent.
	EnsureProject(ctx context.Context, cwd string) (slug, cloneURL string, err error)

	// PrepareSandbox clones the project's repo into a per-sub-agent
	// working directory, checks out a fresh branch, and configures the
	// commit author identity. Returns the sandbox path. Idempotent —
	// re-runs wipe and recreate.
	PrepareSandbox(ctx context.Context, sandboxRoot string, slug string, branch string, agentID string, authorName, authorEmail string, cloneURL string) (path string, err error)

	// CommitAndPush stages every change in sandboxPath, makes a commit
	// (using whatever author the sandbox is configured with), and pushes
	// the named branch upstream. message empty falls back to a default.
	// force allows non-fast-forward updates.
	CommitAndPush(ctx context.Context, sandboxPath, slug, branch, message string, force bool) (commitSHA string, err error)

	// EnsureRemoteBranch creates the branch on the forge if it does not
	// exist (CommitAndPush implicitly creates it via push, but some
	// forges require an explicit creation step before opening a PR).
	// Idempotent — already-exists is not an error.
	EnsureRemoteBranch(ctx context.Context, slug, branch string) error

	// OpenPR opens a PR from branch into the project's main branch,
	// returning the PR number. title/body may be empty (backend
	// supplies defaults).
	OpenPR(ctx context.Context, slug, branch, title, body string) (prNumber int64, err error)

	// ListPRStates returns the current state of every PR whose head
	// branch starts with branchPrefix. The state strings should match
	// the loom.State* constants.
	ListPRStates(ctx context.Context, slug, branchPrefix string) ([]BackendPRState, error)

	// DeleteSandbox removes the sandbox directory rooted at path.
	DeleteSandbox(path string) error

	// DeleteBranch removes the branch from the forge. Best-effort —
	// missing branch is not an error.
	DeleteBranch(ctx context.Context, slug, branch string) error

	// NotifyProjectActive is called at most once per project slug, the
	// first time a lease is created for it. Implementations may use it
	// to lazily start per-project services (e.g. a merger goroutine).
	// Optional: NoOpProjectNotifier is a valid default.
	NotifyProjectActive(ctx context.Context, slug, cloneURL string) error
}

// BackendPRState mirrors the part of a PR loom needs to report status.
type BackendPRState struct {
	PRNumber int64
	HeadRef  string
	State    string // "open" | "closed" | "merged"
	Merged   bool
}
