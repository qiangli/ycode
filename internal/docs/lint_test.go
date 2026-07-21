package docs

import (
	"strings"
	"testing"
)

// TestLint is the CI gate. Every file under agent/ must satisfy every
// curation rule documented in embed.go and enforced in lint.go. If you
// added a new agent doc and this test fails, FIX THE DOC — do not
// loosen the rule.
//
// Cross-reference checks (cobra-subcommand existence) live in the
// cmd/ycode package because they need access to rootCmd.
func TestLint(t *testing.T) {
	violations := Lint()
	if len(violations) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("agent-facing docs failed curation lint:\n")
	for _, v := range violations {
		b.WriteString("  [")
		b.WriteString(v.Topic)
		b.WriteString(" / ")
		b.WriteString(v.Rule)
		b.WriteString("] ")
		b.WriteString(v.Message)
		b.WriteString("\n")
	}
	t.Fatal(b.String())
}

// TestRegistryParses asserts every embedded file is structurally valid
// (parseable frontmatter, matching slug, etc.). This is a tighter check
// than Lint runs: parse errors short-circuit Lint above, but we want a
// dedicated failure mode for the parse stage so editors get a precise
// signal when frontmatter is malformed.
func TestRegistryParses(t *testing.T) {
	if _, err := Registry(); err != nil {
		t.Fatalf("Registry parse failed: %v", err)
	}
}

// TestIndexPresent guards against accidentally removing _index.md.
// `ycode docs` with no args reads this file; without it the command
// returns an empty body and the bootstrap chain breaks.
func TestIndexPresent(t *testing.T) {
	body, err := IndexBody()
	if err != nil {
		t.Fatalf("IndexBody: %v", err)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("_index.md is empty")
	}
}

// TestTopicsStable asserts Topics() returns alphabetically-sorted
// slugs. The shell surface relies on stable ordering so the index a
// human operator sees matches the JSON `--list` output.
func TestTopicsStable(t *testing.T) {
	topics, err := Topics()
	if err != nil {
		t.Fatalf("Topics: %v", err)
	}
	for i := 1; i < len(topics); i++ {
		if topics[i-1] >= topics[i] {
			t.Fatalf("Topics() not sorted: %v", topics)
		}
	}
}
