// Package weaveboard owns the opt-in `ycode weave init-board` flow —
// the only part of weave that touches Gitea's HTML web-routes
// (POST /<owner>/<repo>/projects/new and /columns/new) instead of
// the stable v1 REST API.
//
// Per the docs/loom-v2-implementation.md "Spike 1" resolution, the
// kanban is purely visual — loom does NOT auto-sync card positions on
// state changes, and the default dashboard is the label-filtered issue
// list. This package exists only for users who explicitly run
// `ycode weave init-board` to set up the visual kanban.
//
// Implementation status (N+1 G1): scaffold only. The CSRF + session-
// cookie web-route POST sequence is implementer-known but
// version-coupled to Gitea's UI; the production flow lives in a
// follow-up dedicated PR so this commit doesn't have to be tested
// against a live Gitea web UI. Bootstrap returns a clear deferred
// error until then; the seam (function shape, CLI subverb wiring) is
// in place.
package weaveboard

import (
	"context"
	"errors"
)

// Options carries the inputs Bootstrap needs.
type Options struct {
	// BaseURL is the running Gitea root (e.g. "http://127.0.0.1:5743").
	BaseURL string

	// AdminUser / AdminPass authenticate via the web-route POST
	// /user/login form, yielding a session cookie + CSRF token.
	AdminUser string
	AdminPass string

	// Owner / Repo identify the project to create the board in.
	Owner string
	Repo  string

	// ProjectTitle is the user-visible title of the kanban project
	// (default: "Loom").
	ProjectTitle string
}

// Result describes what Bootstrap created.
type Result struct {
	ProjectID int64
	ColumnIDs map[string]int64 // column title → ID
}

// ErrNotYetImplemented is returned by Bootstrap until the CSRF +
// session flow lands. The CLI subverb (`ycode weave init-board`)
// surfaces this as a clear opt-in-not-wired-yet error envelope.
var ErrNotYetImplemented = errors.New("weaveboard: bootstrap not yet implemented (N+1 G1 ships the seam; CSRF/session flow lands in a follow-up PR)")

// Bootstrap creates the Loom project board in Gitea with the
// canonical column layout matching the loom:* state labels.
//
// Column layout (matches docs/loom-v2-plan.md "First-run setup"):
//   todo · working · submitted · ci_failed · conflict · merged · abandoned
//
// Implementation flow (planned):
//  1. GET /user/login → extract CSRF token from the HTML form.
//  2. POST /user/login with admin creds + CSRF → session cookie.
//  3. GET /<owner>/<repo>/projects/new → fresh CSRF for project create.
//  4. POST /<owner>/<repo>/projects/new with {title, board_type=basickanban}
//     → 302 redirect carries the new project ID in Location.
//  5. For each canonical column: POST /<owner>/<repo>/projects/{id}/columns/new
//     with {title, color}.
//  6. Return Result with project ID + column IDs.
//
// Failure semantics: Bootstrap MUST be idempotent — re-running on a
// project that already has a Loom board returns the existing IDs
// rather than erroring. Implementation detail for the follow-up.
func Bootstrap(ctx context.Context, opts Options) (*Result, error) {
	_ = ctx
	_ = opts
	return nil, ErrNotYetImplemented
}

// CanonicalColumns returns the column titles loom maps to its state
// labels, in display order. Exported so tooling (e.g. tests, doc
// generators) can reference the canonical list without duplicating it.
func CanonicalColumns() []string {
	return []string{"todo", "working", "submitted", "ci_failed", "conflict", "merged", "abandoned"}
}
