package docs

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// classifierOutcomes is the source-of-truth set of outcome_class
// values emitted by internal/runtime/mcpservers/reliability/hints.go's
// classifyOutcome function. Kept here (not imported from reliability)
// because internal/docs MUST NOT depend on a runtime package — the
// docs surface is offline/zero-dep by design. The trade-off: this
// constant has to be hand-updated when the classifier learns a new
// class. The test below catches drift between this list and what
// outcomes.md actually documents, so a renaming or addition that
// touches only one side fails the build.
//
// To add a class: update this list AND outcomes.md (one H2-or-deflist
// entry per class). To rename: do both. To delete: same.
var classifierOutcomes = []string{
	"SUCCESS",
	"AUTH_REDIRECT",
	"BLOCKED",
	"SILENT_CLICK",
}

// TestOutcomeDocCoverage asserts every outcome_class value emitted by
// the runtime classifier appears as a section in outcomes.md, and
// vice-versa. The doc and the runtime drift apart silently otherwise
// — an agent reading outdated prose makes the wrong recovery call.
func TestOutcomeDocCoverage(t *testing.T) {
	doc, err := Get("outcomes")
	if err != nil {
		t.Fatalf("docs.Get(outcomes): %v", err)
	}

	// Outcome names are written in uppercase (SUCCESS, AUTH_REDIRECT, …)
	// and appear either as a definition-list term (line starts with the
	// name and a colon) OR inside a backtick span. Capture every distinct
	// SCREAMING_SNAKE_CASE token in the doc body.
	tokenRe := regexp.MustCompile(`\b[A-Z][A-Z_]+\b`)
	found := map[string]bool{}
	for _, m := range tokenRe.FindAllString(doc.Body, -1) {
		// Filter out incidental uppercase words (URL, JS, etc.) by
		// requiring an underscore OR matching one of the known classes.
		// The classifier names always have underscores or are SUCCESS.
		found[m] = true
	}

	var missing []string
	for _, c := range classifierOutcomes {
		if !found[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("outcomes.md is missing classifier outcomes: %s. "+
			"Update internal/docs/agent/outcomes.md to document them.",
			strings.Join(missing, ", "))
	}

	// Reverse direction: every outcome_class-shaped token the doc
	// mentions must be in classifierOutcomes. Catches docs claiming a
	// class the runtime never emits (e.g. WRONG_ELEMENT, which lives
	// in hints.go's comment but isn't actually returned today).
	known := map[string]bool{}
	for _, c := range classifierOutcomes {
		known[c] = true
	}
	var extra []string
	for tok := range found {
		if !strings.Contains(tok, "_") && tok != "SUCCESS" {
			continue
		}
		if known[tok] {
			continue
		}
		extra = append(extra, tok)
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		t.Fatalf("outcomes.md mentions tokens that look like classifier outcomes "+
			"but are not emitted by hints.go classifyOutcome: %s. "+
			"Either add them to classifierOutcomes (and update hints.go) or remove them from the doc.",
			strings.Join(extra, ", "))
	}
}
