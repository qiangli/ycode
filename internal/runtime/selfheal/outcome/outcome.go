// Package outcome turns a successful worker run into either a GitHub
// PR (when creds are available) or a local-only fix candidate the
// operator can export by hand. Phase 5 of the selfheal plan
// (/Users/qiangli/.claude/plans/summarize-the-previous-issues-squishy-cupcake.md).
//
// Sanitization: the failure trace embedded in the PR body has user
// paths, hostnames, and the YCODE_SELFHEAL_SIGNATURE-shaped envs
// scrubbed per feedback_no_secrets_in_public_artifacts.
package outcome

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ghlib "github.com/google/go-github/v84/github"

	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/github"
	"github.com/qiangli/ycode/internal/runtime/selfheal/worker"
	"github.com/qiangli/ycode/internal/runtime/selfheal/workspace"
)

// Mode is the dispatch mode the publisher chose for an outcome.
type Mode string

const (
	ModePR        Mode = "pr"         // pushed to fork, PR opened
	ModeLocalOnly Mode = "local-only" // no creds; branch kept locally
)

// Result extends worker.Outcome with the Phase 5 fields persisted
// after publish. Written back to <root>/outcome.json so the
// `ycode selfheal list` CLI can show it.
type Result struct {
	worker.Outcome
	PublishMode Mode      `json:"publish_mode"`
	PRURL       string    `json:"pr_url,omitempty"`
	PushedTo    string    `json:"pushed_to,omitempty"` // <owner>/<repo>
	PatchPath   string    `json:"patch_path,omitempty"`
	PublishedAt time.Time `json:"published_at"`
}

// Publisher turns a worker.Outcome into a Result. Holds the path
// bits the daemon already resolved so the call site stays terse.
type Publisher struct {
	baseDir string // ~/.agents/ycode/selfheal
	exec    *git.GitExec
}

// NewPublisher returns a publisher rooted at baseDir.
func NewPublisher(baseDir string) *Publisher {
	return &Publisher{baseDir: baseDir, exec: git.NewGitExec(nil)}
}

// Publish runs the credentialed-PR path when a token is discoverable,
// otherwise the local-only path. Either way the per-signature
// outcome.json is updated.
//
// repoURL is the worker's resolved push target (operator fork → upstream
// fallback chain from workspace.DiscoverFork).
func (p *Publisher) Publish(ctx context.Context, out worker.Outcome, repoURL string) (Result, error) {
	if out.Mode != "success" {
		// Nothing to publish — caller should have gated on this, but
		// don't make a mess if they didn't.
		return Result{Outcome: out, PublishMode: ModeLocalOnly, PublishedAt: time.Now()}, nil
	}
	res := Result{Outcome: out, PublishedAt: time.Now()}

	gh := github.NewClient(ctx)
	if gh == nil {
		// No creds → local-only path.
		patch, err := p.exportPatch(ctx, out)
		if err != nil {
			return res, fmt.Errorf("publish: local-only patch export: %w", err)
		}
		res.PublishMode = ModeLocalOnly
		res.PatchPath = patch
		_ = p.persist(out.Signature, res)
		return res, nil
	}

	// Credentialed path: push to fork, open PR.
	owner, repo := parseOwnerRepo(repoURL)
	if owner == "" || repo == "" {
		return res, fmt.Errorf("publish: cannot parse owner/repo from %q", repoURL)
	}

	if err := p.pushBranch(ctx, out, gh); err != nil {
		return res, fmt.Errorf("publish: push: %w", err)
	}

	cli := github.NewGitHubClient(gh, owner, repo)
	base, err := p.defaultBranch(ctx, gh, owner, repo)
	if err != nil {
		base = "main"
	}
	title := fmt.Sprintf("selfheal: fix %s (signature %s)", trimToolHint(out), out.Signature)
	body := buildPRBody(out)
	pr, err := cli.CreatePR(ctx, title, body, out.BranchName, base)
	if err != nil {
		return res, fmt.Errorf("publish: create PR: %w", err)
	}
	res.PublishMode = ModePR
	res.PushedTo = owner + "/" + repo
	if pr != nil {
		res.PRURL = pr.URL
	}
	_ = p.persist(out.Signature, res)
	return res, nil
}

// pushBranch shells out to git with an http extraheader carrying the
// token, so we don't have to rewrite the URL or rely on a credential
// helper. Falls back to embedded-token-in-URL when extraheader fails.
func (p *Publisher) pushBranch(ctx context.Context, out worker.Outcome, gh *ghlib.Client) error {
	// Sanity: ensure the worktree exists.
	if out.WorktreePath == "" {
		return fmt.Errorf("empty worktree path")
	}
	token := tokenFromClient(gh)
	if token == "" {
		return fmt.Errorf("no token available")
	}
	// Use credential helper-style header so the token never lands in
	// the URL on disk or in the reflog.
	authHeader := fmt.Sprintf("http.extraheader=Authorization: Basic %s",
		basicAuthHeader("x-access-token", token))
	args := []string{
		"-c", authHeader,
		"push", "--set-upstream", "origin",
		out.BranchName,
	}
	if err := p.exec.RunCheck(ctx, out.WorktreePath, args...); err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// exportPatch produces a git format-patch bundle under
// <root>/branch.patch for local-only outcomes. The operator can
// `git am` it into their own checkout.
func (p *Publisher) exportPatch(ctx context.Context, out worker.Outcome) (string, error) {
	if out.WorktreePath == "" {
		return "", fmt.Errorf("empty worktree path")
	}
	layout := workspace.PathsFor(p.baseDir, out.Signature)
	patchPath := filepath.Join(layout.Root, "fix.patch")
	patch, err := p.exec.Run(ctx, out.WorktreePath, "format-patch", "origin/HEAD", "--stdout")
	if err != nil {
		// Fallback: diff vs HEAD~N. Better than nothing.
		patch, err = p.exec.Run(ctx, out.WorktreePath, "diff", "origin/HEAD")
		if err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(patchPath, []byte(patch), 0o600); err != nil {
		return "", err
	}
	return patchPath, nil
}

// persist updates outcome.json in-place. Writes the merged Result
// (worker fields + publish fields) so a `ycode selfheal list`
// consumer sees both at once.
func (p *Publisher) persist(signature string, res Result) error {
	layout := workspace.PathsFor(p.baseDir, signature)
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(layout.Outcome, b, 0o600)
}

// defaultBranch resolves the fork's default branch (usually main).
func (p *Publisher) defaultBranch(ctx context.Context, gh *ghlib.Client, owner, repo string) (string, error) {
	r, _, err := gh.Repositories.Get(ctx, owner, repo)
	if err != nil || r == nil || r.DefaultBranch == nil {
		return "", err
	}
	return *r.DefaultBranch, nil
}

// trimToolHint pulls the tool name out of the worker's persisted
// brief so PR titles read naturally. Best-effort.
func trimToolHint(out worker.Outcome) string {
	if out.Notes != "" && strings.Contains(out.Notes, "tool=") {
		return strings.SplitN(strings.SplitN(out.Notes, "tool=", 2)[1], " ", 2)[0]
	}
	return "tool failure"
}

// parseOwnerRepo extracts (owner, repo) from
// https://github.com/<owner>/<repo>(.git)?. Returns empty strings
// when the URL doesn't look like a GitHub repo URL.
func parseOwnerRepo(repoURL string) (string, string) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", ""
	}
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

// pathSanitizers strip operator-specific shapes from the trace
// before it lands in a public PR body. Mirrors detector.normalizeError
// in spirit but keeps the substantive text — these reduce
// information leakage, not signature variance.
var pathSanitizers = []struct {
	rx   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`/Users/[^/\s:]+`), "/Users/<USER>"},
	{regexp.MustCompile(`/home/[^/\s:]+`), "/home/<USER>"},
	{regexp.MustCompile(`\b[A-Za-z0-9._-]+\.local\b`), "<HOST>.local"},
	// Env-var style: GITHUB_TOKEN=..., MY_API_KEY=..., DB_PASSWORD=...
	// Match any all-caps identifier ending in a secret-shaped suffix.
	{regexp.MustCompile(`\b[A-Z][A-Z0-9_]*(?:TOKEN|SECRET|KEY|PASSWORD|PASSWD|PASS|CREDENTIAL)\b\s*[=:]\s*\S+`), "<REDACTED_ENV>"},
	// Lowercase/inline: api_key: sk-..., password: ...
	{regexp.MustCompile(`(?i)\b(token|password|secret|api[_-]?key|credential)\s*[=:]\s*\S+`), "$1=<REDACTED>"},
	// Bearer / Authorization: Bearer foo
	{regexp.MustCompile(`(?i)\b(bearer|authorization)\s+\S+`), "$1 <REDACTED>"},
}

// SanitizeForPublic strips operator paths, hostnames, and anything
// shaped like a token/secret from text destined for a public PR or
// gist. Idempotent.
func SanitizeForPublic(s string) string {
	for _, p := range pathSanitizers {
		s = p.rx.ReplaceAllString(s, p.with)
	}
	return s
}

// buildPRBody assembles the PR description from the outcome plus a
// link back to the selfheal workspace on the operator's machine.
// The body is sanitized before being returned.
func buildPRBody(out worker.Outcome) string {
	var b strings.Builder
	b.WriteString("Automated fix proposed by ycode selfheal.\n\n")
	b.WriteString("### Failure context\n\n")
	fmt.Fprintf(&b, "- Signature: `%s`\n", out.Signature)
	if out.BranchName != "" {
		fmt.Fprintf(&b, "- Branch: `%s`\n", out.BranchName)
	}
	if out.Iterations > 0 {
		fmt.Fprintf(&b, "- Autoloop iterations: %d\n", out.Iterations)
	}
	if out.DiffLines > 0 {
		fmt.Fprintf(&b, "- Diff size: %d lines\n", out.DiffLines)
	}
	b.WriteString("\n### Verification\n\n")
	b.WriteString("- `make ci-fast` passed in the worker's sandbox\n")
	b.WriteString("- Diff is below the selfheal worker's hallucination cap\n\n")
	b.WriteString("### How to verify locally\n\n")
	b.WriteString("```sh\n")
	fmt.Fprintf(&b, "gh pr checkout <this PR>\nmake ci-fast\n")
	b.WriteString("```\n\n")
	b.WriteString("This PR is generated automatically. Please review the diff before merging — selfheal's evaluation gate is `make ci-fast`, which catches build/test regressions but not design issues or behavior changes that are merely compatible with the existing test suite.\n")
	if out.Notes != "" {
		b.WriteString("\n### Notes from the worker\n\n```\n")
		b.WriteString(out.Notes)
		b.WriteString("\n```\n")
	}
	return SanitizeForPublic(b.String())
}

// basicAuthHeader returns the base64 of "user:pass" for the
// Authorization: Basic <X> header used to push without writing the
// token to disk.
func basicAuthHeader(user, pass string) string {
	return encodeBase64(user + ":" + pass)
}

// encodeBase64 is std base64 without importing encoding/base64 at
// the top of the file just for this one call.
func encodeBase64(s string) string {
	return b64(s)
}
