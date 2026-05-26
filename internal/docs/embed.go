// Package docs is the agent-facing capability catalog. Every file under
// the embedded `agent/` directory is a self-contained prompt instructing
// an agent how to use one ycode capability. The `ycode docs` cobra
// command (cmd/ycode/docs.go) is the sole public entry point.
//
// ============================================================================
// SAFEGUARDS — read before adding or editing a file under agent/
// ============================================================================
//
//  1. AUDIENCE IS AGENTS, NOT HUMANS.
//     Every doc is read by an LLM at runtime to decide what tool to call
//     next. Write in second-person imperative ("When you need X, call Y"),
//     not third-person description ("Users can do X"). If a human would
//     benefit more than an agent, the content belongs in docs/, not here.
//
//  2. THE LINTER (lint.go) IS A CI GATE, NOT A STYLE GUIDE.
//     `go test ./internal/docs/...` will fail the build if any file in
//     agent/ violates the curation rules. Do not relax the rules to make
//     a doc pass — rewrite the doc.
//
//  3. NO LINKS TO INTERNAL STRATEGY.
//     Banned patterns: docs/strategy.md, gap-analysis-*, *-roadmap*,
//     backlog/*. Those documents are human-only; surfacing them in an
//     agent's context window pollutes its decision space.
//
//  4. HARD LINE LIMIT (default 120 lines per doc, configurable per file
//     via the `max_lines` frontmatter key, capped at 200).
//     An agent that has to scan 500 lines to find one verb will use the
//     wrong tool. Curate ruthlessly. If a capability legitimately needs
//     more than 200 lines, split it into two topics.
//
//  5. EVERY DOC ENDS WITH `## Exact calls`.
//     Copy-pasteable invocations (yc commands, mcp__ycode__* tool calls,
//     HTTP curl examples). Without this section the agent has to invent
//     the syntax — which it will, badly.
//
//  6. ONE TOPIC PER FILE; FILENAME == TOPIC SLUG.
//     agent/loom.md → topic "loom". The slug must match `^[a-z][a-z0-9-]*$`.
//     The special file _index.md is the human-curated topic index shown
//     by `ycode docs` with no args; it is NOT a topic itself.
//
//  7. NO RUNTIME FILESYSTEM READS.
//     Everything is embedded via go:embed. `ycode docs` works in any cwd,
//     on any host, with no install paths or config. Do not add a code
//     path that reads ~/.config/ycode/docs/ or similar — bootstrap relies
//     on this command being available offline and read-only.
//
//  8. DISCOVERY SURFACES ARE FIXED. AGENT vs HUMAN AUDIENCES STAY SEPARATE.
//     Agent surfaces (read the curated prompts here):
//     (a) `ycode docs` shell command (cmd/ycode/docs.go),
//     (b) `mcp__ycode__list_docs` / `mcp__ycode__get_doc` MCP tools
//     (internal/docs/mcpserver.go; mounted into both the stdio
//     composite at cmd/ycode/mcp.go and the HTTP composite at
//     cmd/ycode/serve.go),
//     (c) `ycode://docs/<slug>` MCP resources (same handler),
//     (d) one-liner in .agents/ycode/AGENTS.md (TODO: extend selfinit).
//     Human surfaces (auto-generated from cobra):
//     (d) `ycode help` / `ycode <cmd> --help`,
//     (e) `yc help` shell built-in list.
//     The two sets cross-reference each other but do not share content:
//     help is structural (flags, args, usage); docs is prose curated for
//     LLM decision-making. The linter (lint.go) asserts every cobra
//     subcommand name appearing in any agent/*.md resolves to a real
//     rootCmd subcommand — that is the consistency gate.
//     Do not add a sixth surface without updating this list.
//
// ============================================================================
package docs

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
)

// agentFS holds every file under agent/. The `all:` prefix ensures
// files starting with `_` (such as _index.md) are included; without it
// the go:embed directive silently skips them.
//
//go:embed all:agent
var agentFS embed.FS

// IndexSlug is the reserved filename (without extension) for the
// hand-curated topic index shown by `ycode docs` with no arg. It is
// excluded from the parsed topic registry to keep it from showing up
// as a queryable topic.
const IndexSlug = "_index"

// MaxLinesCap is the hard upper bound for any doc's max_lines override.
// A doc that legitimately needs more than this should be split.
const MaxLinesCap = 200

// DefaultMaxLines is applied when a doc omits max_lines from frontmatter.
const DefaultMaxLines = 120

// Doc is one parsed agent-facing capability document.
type Doc struct {
	Topic    string // slug; matches filename minus .md
	Summary  string // ≤60 chars, shown in index
	When     string // trigger conditions, shown in index
	MaxLines int    // line cap (DefaultMaxLines unless overridden)
	Body     string // markdown body (frontmatter stripped)
	Raw      string // the entire file including frontmatter, for --all
}

// IndexBody returns the hand-curated _index.md body verbatim. This is
// what `ycode docs` (no args) prints. The index is NOT auto-generated
// from the topic registry on purpose: a human chooses the ordering and
// the one-line hooks so the agent's first read is optimized.
func IndexBody() (string, error) {
	b, err := agentFS.ReadFile("agent/_index.md")
	if err != nil {
		return "", fmt.Errorf("docs: read _index.md: %w", err)
	}
	return string(b), nil
}

// registry is the parsed set of topics, populated once on first access.
var (
	registryOnce sync.Once
	registry     map[string]*Doc
	registryErr  error
)

// Registry returns the parsed topic registry keyed by slug. The first
// call parses every embedded file under agent/ except _index.md. Parse
// errors are surfaced once and cached — they indicate a malformed
// frontmatter that the linter should have caught at build time.
func Registry() (map[string]*Doc, error) {
	registryOnce.Do(func() {
		registry, registryErr = parseAll(agentFS)
	})
	return registry, registryErr
}

// Topics returns every topic slug in stable alphabetical order. Useful
// for the linter, the --list flag, and any caller that wants to iterate
// without depending on map order.
func Topics() ([]string, error) {
	reg, err := Registry()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(reg))
	for slug := range reg {
		out = append(out, slug)
	}
	sort.Strings(out)
	return out, nil
}

// Get returns the parsed doc for a topic slug, or an error if no such
// topic exists. Callers that need to suggest "did you mean…" should
// use Topics() to enumerate available slugs.
func Get(slug string) (*Doc, error) {
	reg, err := Registry()
	if err != nil {
		return nil, err
	}
	d, ok := reg[slug]
	if !ok {
		return nil, fmt.Errorf("docs: unknown topic %q", slug)
	}
	return d, nil
}

// parseAll walks the embedded agent/ directory, parses every .md file
// except _index.md, and returns a slug-keyed registry. Used by Registry
// (production) and by the linter test (which re-parses to assert rules).
func parseAll(fsys fs.FS) (map[string]*Doc, error) {
	out := map[string]*Doc{}
	entries, err := fs.ReadDir(fsys, "agent")
	if err != nil {
		return nil, fmt.Errorf("docs: read agent/: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		slug := strings.TrimSuffix(name, ".md")
		if slug == IndexSlug {
			continue
		}
		raw, err := fs.ReadFile(fsys, "agent/"+name)
		if err != nil {
			return nil, fmt.Errorf("docs: read %s: %w", name, err)
		}
		doc, err := parseDoc(slug, string(raw))
		if err != nil {
			return nil, fmt.Errorf("docs: parse %s: %w", name, err)
		}
		out[slug] = doc
	}
	return out, nil
}
