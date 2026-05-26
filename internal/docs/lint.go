package docs

import (
	"fmt"
	"regexp"
	"strings"
)

// LintResult is one violation found by Lint. Multiple violations are
// returned together so a single test run surfaces every problem rather
// than one at a time. Tests should fail the build when len(violations)
// > 0; the slice itself never carries a non-violation entry.
type LintResult struct {
	Topic   string // slug being linted, or "_index" for the index file
	Rule    string // short identifier, e.g. "max_lines", "banned_link"
	Message string // human-readable explanation including the offending text
}

// Lint runs every curation rule against the embedded agent/ directory.
// Returns nil when every rule passes. Designed to be called from
// `go test ./internal/docs/...` as the build-blocking CI gate.
//
// Rules enforced here (each MUST stay in lock-step with the safeguard
// comment in embed.go):
//
//   - frontmatter parseable, slug matches filename
//   - line count ≤ max_lines (default 120, cap 200)
//   - no links to docs/strategy.md, gap-analysis-*, *-roadmap*, backlog/*
//   - body contains an "## Exact calls" H2 section
//   - body contains a "## When to use" H2 section (also accepts the
//     equivalent "## When to use this")
//
// Cross-reference rules (every `ycode <subcommand>` mention resolves to
// a real rootCmd subcommand) live in cmd/ycode/docs_test.go — they
// require importing the cobra tree, which would create a circular
// dependency from internal/docs back to cmd/ycode.
func Lint() []LintResult {
	var out []LintResult

	// _index.md gets a lighter rule set: no frontmatter required (it is
	// a hand-curated landing page, not a registered topic), but it
	// still must avoid the banned-link patterns and must mention the
	// cross-reference to `ycode help`.
	if idx, err := IndexBody(); err == nil {
		out = append(out, lintIndex(idx)...)
	} else {
		out = append(out, LintResult{
			Topic: IndexSlug, Rule: "missing", Message: err.Error(),
		})
	}

	// Reparse via the embedded FS so the linter exercises the same
	// code path the runtime uses. A frontmatter error here means a
	// file failed parseDoc; surface it as a lint violation rather than
	// crashing the test.
	reg, err := Registry()
	if err != nil {
		out = append(out, LintResult{
			Topic: "(parse)", Rule: "parse", Message: err.Error(),
		})
		return out
	}

	for slug, doc := range reg {
		out = append(out, lintDoc(slug, doc)...)
	}
	return out
}

// bannedLinkPatterns enumerates internal-strategy doc references that
// must never appear in agent-facing prompts. The matcher is a simple
// substring scan, not regex, so each pattern is the literal token an
// editor would paste.
//
// SAFEGUARD: adding to this list is fine. REMOVING from this list
// requires updating safeguard #3 in embed.go and explaining why in
// the commit message — the rationale is that internal strategy noise
// degrades LLM tool selection. Don't relax silently.
var bannedLinkPatterns = []string{
	"docs/strategy.md",
	"gap-analysis-",
	"-roadmap",
	"docs/backlog",
	"backlog/",
	"lighthouse-roadmap",
}

var (
	exactCallsHeader = regexp.MustCompile(`(?m)^##\s+Exact calls\s*$`)
	whenHeader       = regexp.MustCompile(`(?m)^##\s+When to use(\s+this)?\s*$`)
)

func lintDoc(slug string, d *Doc) []LintResult {
	var out []LintResult

	// Line count cap — counted on Raw (frontmatter + body) so an author
	// can't game the limit by stuffing content into frontmatter.
	lineCount := strings.Count(d.Raw, "\n") + 1
	if lineCount > d.MaxLines {
		out = append(out, LintResult{
			Topic: slug, Rule: "max_lines",
			Message: fmt.Sprintf("%d lines exceeds max_lines=%d (cap %d). Curate or split.",
				lineCount, d.MaxLines, MaxLinesCap),
		})
	}

	// Summary length budget. The index shows summaries one-per-line; if
	// they wrap they stop being scannable. 60 chars matches the schema.
	if n := len(d.Summary); n > 60 {
		out = append(out, LintResult{
			Topic: slug, Rule: "summary_length",
			Message: fmt.Sprintf("summary is %d chars; cap is 60.", n),
		})
	}

	// Required body sections.
	if !whenHeader.MatchString(d.Body) {
		out = append(out, LintResult{
			Topic: slug, Rule: "missing_section",
			Message: `body must contain an H2 "## When to use" (or "## When to use this") section.`,
		})
	}
	if !exactCallsHeader.MatchString(d.Body) {
		out = append(out, LintResult{
			Topic: slug, Rule: "missing_section",
			Message: `body must end with an "## Exact calls" H2 section with copy-pasteable invocations.`,
		})
	}

	// Banned internal-strategy links.
	for _, pat := range bannedLinkPatterns {
		if strings.Contains(d.Body, pat) {
			out = append(out, LintResult{
				Topic: slug, Rule: "banned_link",
				Message: fmt.Sprintf("body references %q — internal-strategy docs must not appear in agent prompts.", pat),
			})
		}
	}

	return out
}

func lintIndex(body string) []LintResult {
	var out []LintResult
	for _, pat := range bannedLinkPatterns {
		if strings.Contains(body, pat) {
			out = append(out, LintResult{
				Topic: IndexSlug, Rule: "banned_link",
				Message: fmt.Sprintf("_index.md references %q.", pat),
			})
		}
	}
	// The index MUST point operators at `ycode help`. This is the
	// human/agent cross-reference half of safeguard #8 in embed.go.
	if !strings.Contains(body, "ycode help") {
		out = append(out, LintResult{
			Topic: IndexSlug, Rule: "missing_xref",
			Message: `_index.md must mention "ycode help" so human operators are routed to the cobra surface.`,
		})
	}
	return out
}
