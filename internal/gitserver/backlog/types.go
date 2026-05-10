//go:build experimental

// Package backlog manages the markdown-as-source-of-truth task spec at
// docs/backlog/. The reconciler synchronizes those files into Gitea
// issues in admin/<slug> via the queue package.
//
// Source-of-truth direction: docs/backlog/ → Gitea (one-way at create
// time; both ways for state transitions). Gitea is a derived
// coordination cache that can be wiped and rebuilt from the markdown
// at any time.
//
// This package is host-side only. It is never linked into the Worker
// tool surface — Workers receive their brief via CLI flags from the
// Foreman and never read docs/backlog/.
//
// See docs/backlog.md.
package backlog

import "time"

// Priority labels accepted in frontmatter. Mirror queue.LabelP1/P2/P3
// values so the reconciler can pass them through to queue.Submit.
const (
	PriorityP1 = "p1"
	PriorityP2 = "p2"
	PriorityP3 = "p3"
)

// State values for the markdown-side `state:` frontmatter field.
const (
	StateOpen       = "open"
	StateInProgress = "in_progress"
	StateDone       = "done"
)

// SlugMarkerPrefix and SlugMarkerSuffix bracket the slug marker that
// reconcile injects into the Gitea issue body. This lets reconcile
// re-link a markdown file to its Gitea issue if the writeback of
// `gitea_issue:` to frontmatter was lost (e.g. crash between Submit
// and the writeback).
const (
	SlugMarkerPrefix = "<!-- backlog-slug: "
	SlugMarkerSuffix = " -->"
)

// Issue is the in-memory representation of one docs/backlog/<slug>.md
// file. Round-trips through Load → (mutate) → render with full
// fidelity for the fields below; arbitrary unknown frontmatter keys
// are NOT preserved (intentional: keeps the schema disciplined).
type Issue struct {
	Slug       string    // filename stem (kebab-case); never serialized into frontmatter (it IS the filename)
	Title      string    // human-readable title
	Priority   string    // p1|p2|p3
	State      string    // open|in_progress|done
	Created    time.Time // first time this issue was authored
	GiteaIssue *int64    // populated by reconcile after Submit; nullable on freshly-authored markdown
	Acceptance []string  // optional acceptance criteria; rendered into Gitea body as "## Acceptance"
	Body       string    // freeform markdown after the frontmatter (no slug marker, no acceptance section)
	Path       string    // absolute disk path; not serialized
}

// IsValidPriority reports whether s is one of p1, p2, p3.
func IsValidPriority(s string) bool {
	switch s {
	case PriorityP1, PriorityP2, PriorityP3:
		return true
	}
	return false
}

// IsValidState reports whether s is one of open, in_progress, done.
func IsValidState(s string) bool {
	switch s {
	case StateOpen, StateInProgress, StateDone:
		return true
	}
	return false
}
