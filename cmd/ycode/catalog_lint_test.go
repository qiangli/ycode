package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/docs"
	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// TestCatalogLint is the parity gate for internal/docs/catalog.yaml.
// Three drift modes it catches:
//
//  1. cli: entries that name a `ycode <verb>` that no longer exists.
//  2. mcp: entries that name a tool the runtime doesn't actually
//     register (e.g. a tool was renamed; the catalog now lies).
//  3. read_more: targets that point at a topic slug with no
//     corresponding agent/<slug>.md file (a soft warn — printed via
//     t.Log so docs can roll out across PRs).
//
// Lives in package main (not internal/docs) so it can resolve cobra
// subcommand names against the real rootCmd without an import cycle.
func TestCatalogLint(t *testing.T) {
	cat, err := docs.LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	knownCobra := map[string]bool{}
	collectCommandNames(rootCmd, knownCobra)
	for _, builtin := range []string{"help", "completion"} {
		knownCobra[builtin] = true
	}

	knownMCP := knownMCPToolNames(t)

	knownTopics := map[string]bool{}
	topics, err := docs.Topics()
	if err != nil {
		t.Fatalf("docs.Topics: %v", err)
	}
	for _, slug := range topics {
		knownTopics[slug] = true
	}

	cliVerb := regexp.MustCompile(`^ycode\s+([a-z][a-z0-9-]*)`)

	var violations []string
	for _, row := range cat.Rows {
		for _, c := range row.Surfaces["cli"] {
			m := cliVerb.FindStringSubmatch(c)
			if m == nil {
				violations = append(violations,
					fmt.Sprintf("row %q: cli entry %q does not start with `ycode <verb>`", row.Task, c))
				continue
			}
			if !knownCobra[m[1]] {
				violations = append(violations,
					fmt.Sprintf("row %q: cli entry %q references unknown subcommand %q", row.Task, c, m[1]))
			}
		}
		for _, name := range row.Surfaces["mcp"] {
			if !knownMCP[name] {
				violations = append(violations,
					fmt.Sprintf("row %q: mcp entry %q is not registered on any known transport", row.Task, name))
			}
		}
		if row.ReadMore != "" && !knownTopics[row.ReadMore] {
			t.Logf("row %q: read_more %q has no agent/%s.md yet (soft warn)",
				row.Task, row.ReadMore, row.ReadMore)
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatal("catalog.yaml drift:\n  " + strings.Join(violations, "\n  "))
	}
}

// knownMCPToolNames assembles the union of MCP tool names that ycode
// can advertise across its two transports. Source:
//
//   - the docs handler itself (list_docs, get_doc, list_catalog), via
//     its in-process ListTools().
//   - the http-only set declared by internal/runtime/mcp/
//     crossTransportTools (browser_*, loom_*, memex_*, repomap_*,
//     observability).
//   - a small explicit stdio-only set for treesitter and skills tools
//     that aren't crossTransportTools members (they exist on stdio
//     too, so the cross-transport hint stays silent for them).
//
// Updating this list is fine. The whole point of the test is to
// surface drift — if a row in catalog.yaml names a tool that none of
// these sets contain, either add the tool to its source-of-truth
// registration or drop the row.
func knownMCPToolNames(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}

	for _, tool := range docs.NewMCPHandler().ListTools() {
		out[tool.Name] = true
	}

	for name := range mcp.CrossTransportTools() {
		out[name] = true
	}

	for _, name := range []string{
		// Treesitter — mounted on BOTH transports.
		"list_symbols",
		"search_symbols_by_pattern",
		"get_supported_languages",
		// Skills — mounted on both.
		"list_skills",
		"get_skill",
		// Memex — mounted on both.
		"memex_save",
		"memex_recall",
		"memex_list",
		"memex_forget",
		"memex_index",
		"search_memex",
		"list_memory_types",
		// Repomap — mounted on both.
		"build_repomap",
		"repomap_for_files",
		// Sandbox — mounted on both (internal/container/mcpserver.go).
		"sandbox_exec",
	} {
		out[name] = true
	}

	return out
}
