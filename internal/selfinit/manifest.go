package selfinit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DefaultPort is the proxy port `ycode serve` listens on by default.
// SelfInit uses this when no manifest is readable to register HTTP
// MCP entries optimistically.
//
// Chosen to sit below the OS ephemeral-port pool on both Linux
// (default ip_local_port_range 32768–60999) and macOS (49152–65535),
// so a fresh `ycode serve` cannot race-lose to an OS-assigned
// ephemeral socket. IANA-unassigned; pi mnemonic.
const DefaultPort = 31415

// ManifestPath returns the canonical location ycode serve writes its
// manifest to (~/.agents/ycode/manifest.json).
func ManifestPath(home string) string {
	return filepath.Join(home, ".agents", "ycode", "manifest.json")
}

// LoadCapabilities reads the manifest at home and produces the list of
// MCP servers SelfInit should register. If the manifest is missing or
// unparseable, it falls back to a baseline derived from defaultPort.
//
// Names are stable — same family always gets the same "ycode-<family>"
// name across runs, so writes stay idempotent.
func LoadCapabilities(home string, defaultPort int) []CapabilitySpec {
	if defaultPort <= 0 {
		defaultPort = DefaultPort
	}
	caps, ok := tryLoadManifest(ManifestPath(home))
	if ok {
		return caps
	}
	return baselineCapabilities(defaultPort)
}

// tryLoadManifest parses ~/.agents/ycode/manifest.json. Returns
// (nil, false) on any failure — callers should fall through to the
// baseline.
func tryLoadManifest(path string) ([]CapabilitySpec, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var m manifestShape
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false
	}
	var out []CapabilitySpec

	if m.MCP.Stdio.Command != "" {
		// Stdio command from the manifest is "ycode" by convention; we
		// override that with bootstrap detection so that callers without
		// ycode on PATH still work.
		cmd, args := DetectYcodeCommand(m.MCP.Stdio.Command, m.MCP.Stdio.Args)
		out = append(out, CapabilitySpec{
			Name:      "ycode-stdio",
			Transport: "stdio",
			Command:   cmd,
			Args:      args,
			Family:    "stdio",
		})
	}

	families := make([]string, 0, len(m.MCP.HTTP))
	for f := range m.MCP.HTTP {
		families = append(families, f)
	}
	sort.Strings(families) // deterministic output for tests
	for _, f := range families {
		url := m.MCP.HTTP[f]
		if url == "" {
			continue
		}
		// The composite "ycode" key is the only HTTP MCP entry as of
		// schemaVersion 4. Avoid the ugly "ycode-ycode" double name.
		name := "ycode-" + f
		if f == "ycode" {
			name = "ycode"
		}
		out = append(out, CapabilitySpec{
			Name:      name,
			Transport: "http",
			URL:       url,
			Family:    f,
		})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// baselineCapabilities is what we register when no manifest is readable.
// HTTP entries point at the default port; foreign tools' first call will
// fail with connection-refused until `ycode serve` is up, but the wiring
// is already in place — no second `ycode init` needed.
//
// As of manifest schemaVersion 4 there is a single composite HTTP MCP
// endpoint at /mcp/ that fans out to every family (treesitter, skills,
// gitea, loom, pulse). Per-family routes were retired.
func baselineCapabilities(port int) []CapabilitySpec {
	stdioCmd, stdioArgs := DetectYcodeCommand("ycode", []string{"mcp", "serve"})
	return []CapabilitySpec{
		{Name: "ycode-stdio", Transport: "stdio", Command: stdioCmd, Args: stdioArgs, Family: "stdio"},
		{Name: "ycode", Transport: "http", URL: fmt.Sprintf("http://127.0.0.1:%d/mcp/", port), Family: "ycode"},
	}
}

// FamilyDescription returns a human-friendly one-line description for
// the given family, used in L2 instruction blocks. Unknown families
// fall through to a generic "see ycode docs" line.
func FamilyDescription(family string) string {
	if d, ok := familyDescriptions[family]; ok {
		return d
	}
	return "see https://github.com/qiangli/ycode for details"
}

var familyDescriptions = map[string]string{
	"stdio":     "treesitter AST search (`list_symbols`, `search_symbols_by_pattern`, `find_symbol_references`). Prefer over `grep` when the language is supported (Go, Python, JS/TS, Rust, Java, C, Ruby). The stdio entry also exposes the M1 families below (repomap, codegraph, sandbox, github) — same process.",
	"loom":      "workspace substrate. When the user asks for parallel sub-agents, refactors that touch many files, or anything that benefits from isolated git workspaces, lease one workspace per sub-agent via `loom_lease`. Push with `loom_push`, open a PR with `loom_merge`, poll `loom_status`. Do not mutate cwd in parallel branches; sub-agents will collide.",
	"pulse":     "observability stack (~25 tools): traces, logs, metrics, alerts, dashboards. Query pulse instead of grepping logs on disk.",
	"gitea":     "local Gitea forge ops (~11 tools): repos, branches, PRs, issues. Use these instead of shelling out to `gh` or `git`.",
	"repomap":   "token-budgeted file→symbol overview (`build_repomap`). Call once early in a session to seed system-prompt context for unfamiliar codebases; pass `query` to rank files by relevance.",
	"codegraph": "code-knowledge graph (gfy): summary, semantic query, neighbors, god nodes, shortest path. Loads `.agents/ycode/graph.json` if present, otherwise builds on first call. Use to answer architectural questions (\"what touches the auth flow?\") without grepping.",
	"sandbox":   "podman-isolated execution (`sandbox_exec`). Alpine by default, network=none, cwd mounted at /workspace. Use for running untrusted or AI-generated code without exposing the host filesystem or network.",
	"github":    "GitHub PRs, issues, and CI checks (`github_list_prs`, `github_get_pr_diff`, `github_create_pr_review`, `github_get_check_runs`, ...). Auth from GITHUB_TOKEN, GH_TOKEN, or `~/.config/gh/hosts.yml` — no `gh` binary required.",
}

// manifestShape is the slice of ~/.agents/ycode/manifest.json we read.
// Mirrors the format written by cmd/ycode/manifest.go:writeServeManifest.
type manifestShape struct {
	SchemaVersion string      `json:"schemaVersion"`
	MCP           manifestMCP `json:"mcp"`
}

type manifestMCP struct {
	Stdio manifestStdio     `json:"stdio"`
	HTTP  map[string]string `json:"http"`
}

type manifestStdio struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}
