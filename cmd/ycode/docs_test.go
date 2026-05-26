package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/docs"
)

// TestDocsCobraCrossRef is the help/docs consistency gate. Every
// CODE-FENCED `ycode <subcommand>` token mentioned in any agent doc MUST
// resolve to a real subcommand under rootCmd. This catches the drift
// mode where someone renames a cobra command in main.go but forgets to
// update the agent-facing prose.
//
// Why backtick-fenced only: agents copy verbatim from code spans, not
// English prose. Restricting the regex to backticked mentions sidesteps
// false positives like "ycode is great" or "ycode ships two servers"
// while still catching every literal invocation an agent will execute.
//
// Lives in package main (not internal/docs) because resolving cobra
// subcommands requires the rootCmd tree, and importing cmd/ycode from
// internal/docs would invert the dependency direction.
//
// Allowlist intentionally narrow: only the leading word after "ycode "
// counts. `ycode docs --all` checks "docs"; `ycode mcp serve` checks
// "mcp". Sub-subcommand drift (e.g., renaming `mcp serve` to `mcp run`)
// is not caught by this test today — add a deeper check if a future
// rename actually causes pain.
func TestDocsCobraCrossRef(t *testing.T) {
	known := map[string]bool{}
	collectCommandNames(rootCmd, known)
	// Cobra built-ins registered lazily, not always visible via
	// Commands() in unit tests. Keep this list short and obvious.
	for _, builtin := range []string{"help", "completion"} {
		known[builtin] = true
	}

	// Match `ycode <verb>` only inside single backticks. Captures the
	// first verb token; flags, sub-subcommands, and trailing args are
	// ignored for this consistency check.
	pattern := regexp.MustCompile("`ycode\\s+([a-z][a-z0-9-]*)[^`]*`")

	topics, err := docs.Topics()
	if err != nil {
		t.Fatalf("docs.Topics: %v", err)
	}

	var violations []string
	check := func(label, body string) {
		seen := map[string]bool{}
		for _, m := range pattern.FindAllStringSubmatch(body, -1) {
			sub := m[1]
			if seen[sub] {
				continue
			}
			seen[sub] = true
			// Subcommand allowlist exceptions: flags that look like
			// subcommands ("--help"), and the literal `ycode` binary
			// reference with no verb. The regex already excludes
			// those, but if you find a false positive, document the
			// exception here rather than relaxing the regex.
			if !known[sub] {
				violations = append(violations,
					fmt.Sprintf("%s: references `ycode %s` but no such subcommand exists in rootCmd",
						label, sub))
			}
		}
	}

	if idx, err := docs.IndexBody(); err == nil {
		check("_index.md", idx)
	}
	for _, slug := range topics {
		d, err := docs.Get(slug)
		if err != nil {
			t.Fatalf("docs.Get(%q): %v", slug, err)
		}
		check(slug+".md", d.Body)
	}

	if len(violations) > 0 {
		t.Fatal("agent-docs / cobra drift detected:\n  " +
			strings.Join(violations, "\n  ") +
			"\nFix: rename the doc reference, or restore the cobra command name.")
	}
}

// collectCommandNames walks the cobra tree rooted at root and records
// every command's Name() into known. Includes the root itself so a doc
// referencing the bare `ycode` invocation does not trigger a false
// positive (though the regex requires a subcommand token after "ycode",
// so this is defense-in-depth only).
func collectCommandNames(root *cobra.Command, known map[string]bool) {
	known[root.Name()] = true
	for _, c := range root.Commands() {
		collectCommandNames(c, known)
	}
}
