package selfinit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// claude is the Tool implementation for Anthropic's Claude Code CLI.
type claude struct{}

// init registers Claude Code with the package-level tool registry. The
// auto-startup hook iterates the registry; explicit callers can pass
// their own list via Options.Tools.
func init() {
	RegisterTool(&claude{})
}

func (c *claude) Name() string { return "claude" }

// Detect reports whether Claude Code looks installed on this host.
// True if `claude` is on PATH or `~/.claude/` exists. We don't require
// both — a freshly-installed Claude Code may not have created the user
// dir yet, and a user without the binary on PATH may still have a dir
// from a prior install.
func (c *claude) Detect() bool {
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
		return true
	}
	return false
}

// claudeUserConfigPath returns the path Claude Code reads at user
// scope. Documented as ~/.claude.json by Claude Code.
func claudeUserConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// claudeUserMemoryPath returns the path Claude Code reads at user
// scope for context/memory injection.
func claudeUserMemoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "CLAUDE.md"), nil
}

// claudeMCPServer is the JSON shape of one entry under "mcpServers"
// in ~/.claude.json. Stdio uses {command, args}; HTTP uses {type, url}.
type claudeMCPServer struct {
	// Stdio:
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`

	// HTTP / SSE:
	Type string `json:"type,omitempty"`
	URL  string `json:"url,omitempty"`
}

// WriteMCP merges ycode's capability list into ~/.claude.json under
// the "mcpServers" key. Existing non-ycode entries are preserved.
func (c *claude) WriteMCP(_ context.Context, caps []CapabilitySpec) (bool, error) {
	path, err := claudeUserConfigPath()
	if err != nil {
		return false, err
	}

	// Read-merge-write to preserve user's other servers and other
	// top-level keys Claude Code may stash in this file.
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &root)
	}

	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	// Drop any ycode-* entries that we are NOT going to re-register
	// (e.g. a family that disappeared from the manifest).
	desiredNames := make(map[string]bool, len(caps))
	for _, cs := range caps {
		desiredNames[cs.Name] = true
	}
	for name := range servers {
		if strings.HasPrefix(name, "ycode-") && !desiredNames[name] {
			delete(servers, name)
		}
	}

	// Add or replace each desired entry.
	for _, cs := range caps {
		entry := claudeMCPServer{}
		switch cs.Transport {
		case "stdio":
			entry.Command = cs.Command
			entry.Args = append([]string(nil), cs.Args...)
		case "http":
			entry.Type = "http"
			entry.URL = cs.URL
		default:
			continue
		}
		// Encode through json so the resulting servers map has plain
		// map[string]any values (avoiding type-mismatch churn on the
		// next read-merge cycle).
		buf, _ := json.Marshal(entry)
		var generic map[string]any
		_ = json.Unmarshal(buf, &generic)
		servers[cs.Name] = generic
	}
	root["mcpServers"] = servers

	// Stable key ordering for diffability and idempotency.
	out, err := marshalStable(root)
	if err != nil {
		return false, err
	}

	return writeIfChanged(path, out)
}

// WriteInstructions splices the L2 awareness block into Claude Code's
// user-scope memory file (~/.claude/CLAUDE.md).
func (c *claude) WriteInstructions(_ context.Context, caps []CapabilitySpec) (bool, error) {
	path, err := claudeUserMemoryPath()
	if err != nil {
		return false, err
	}
	body := buildInstructionsBlock(caps)
	existing, _ := os.ReadFile(path)
	new := SpliceBlock(string(existing), body)
	return writeIfChanged(path, []byte(new))
}

// buildInstructionsBlock constructs the manifest-derived L2 content
// that lands inside <BEGIN/END YCODE> in foreign tools' memory files.
//
// The block has two halves:
//
//  1. **MCP capabilities** — manifest-derived, named `ycode-<family>`.
//     Reachable only when the foreign tool has loaded the corresponding
//     MCP server entry from `~/.claude.json` / `~/.config/opencode/mcp.json`
//     etc. (managed automatically by SelfInit's WriteMCP).
//
//  2. **`yc <verb>` shell built-ins** — bash-callable commands that
//     ride the same Go code as the MCP handlers but require zero MCP
//     setup. Active in two scenarios:
//
//     * Foreign tool's bash backend points at `ycode shell -c` (or
//     a PATH wrapper that does — see docs/shell-agent.md).
//     * The user types `ycode shell -c "yc <verb> ..."` manually.
//
//     The yc family is the recommended primary integration path because
//     it works without MCP-server consent prompts and is debuggable as
//     plain bash output.
func buildInstructionsBlock(caps []CapabilitySpec) string {
	var b strings.Builder
	b.WriteString("## ycode capabilities\n\n")
	b.WriteString("ycode runs locally and exposes its capabilities through two reachable surfaces. Prefer them over generic shell tools when the language/use-case matches.\n\n")

	b.WriteString("### MCP servers (when the corresponding entry is in your tool's mcpServers config)\n\n")
	for _, cs := range caps {
		fmt.Fprintf(&b, "- **`%s`** — %s\n", cs.Name, FamilyDescription(cs.Family))
	}

	b.WriteString("\n### `yc <verb>` shell built-ins (bash-callable, zero MCP setup)\n\n")
	b.WriteString("Active whenever your bash backend routes through `ycode shell -c` (e.g., a PATH wrapper at `~/bin/ycode-wrappers/bash`, or direct `ycode shell -c \"...\"` invocation). These are the same Go capabilities as the MCP family, just reachable as plain bash commands. Use them by default — they don't require MCP-server consent and the agent-mode hint engine surfaces suggestions on stderr.\n\n")
	b.WriteString("Verbs are ordered by typical ROI; top four carry a *why* note so the choice over `grep`/`find`/`git` is explicit.\n\n")
	b.WriteString("- `yc symbols <path>` — list top-level symbols (treesitter; faster than ctags). Replaces `ctags -R` and most `grep -r` for declarations.\n")
	b.WriteString("  *Why over grep:* AST-aware; skips comments/string literals; resolves aliases.\n")
	b.WriteString("- `yc search-symbols <pattern> [path]` — name-substring symbol search across the workspace.\n")
	b.WriteString("  *Why over `grep -r`:* scopes to declared identifiers; no false positives in vendored copies.\n")
	b.WriteString("- `yc repomap [--budget=N]` — token-budgeted file→symbol overview. Replaces `find . -name *.go | xargs head` for repo orientation.\n")
	b.WriteString("  *Why over `find` + `head`:* files ranked by symbol density; one call instead of N.\n")
	b.WriteString("- `yc refs <symbol>` — find references and callers across the workspace.\n")
	b.WriteString("  *Why over `grep -rn 'FuncName('`:* AST-aware; skips comments; follows aliasing.\n")
	b.WriteString("- `yc git <subcommand>` — native go-git (no fork). Faster than shelling out for `git log/status/diff/branch/show/blame`.\n")
	b.WriteString("- `yc test [--json] -- <args>` — language-aware test runner with structured output (auto-detects go/pytest/jest/cargo/mvn).\n")
	b.WriteString("- `yc lsp <action> <file>:<line>:<col>` — LSP hover/diagnostics/rename/definition/refs.\n")
	b.WriteString("- `yc run --json -- <cmd>` — structured-envelope wrapper around exec (stdout/stderr/exit/duration as JSON).\n")
	b.WriteString("- `yc qacache <action>` — inspect/manage the project-local Q→A cache.\n")
	b.WriteString("- `yc graph \"<DQL>\"` — read-only DQL query against ycode's code knowledge graph (uses persistent DB from `ycode serve`, falls back to ephemeral treesitter pass for small workspaces).\n")
	b.WriteString("- `yc remember \"<text>\"` / `yc recall <query>` — semantic memory (RRF fusion across memex backends). When `CLAUDE_PROJECT_DIR` is set, writes through to Claude Code's `~/.claude/projects/<id>/memory/`.\n")
	b.WriteString("- `yc sandbox -- <cmd>` — podman-isolated execution (alpine, network=none, cwd-mounted). With `YCODE_AUTO_SANDBOX=1`, destructive patterns route through this automatically.\n")
	b.WriteString("- `yc help` / `yc manifest` — discovery.\n\n")

	b.WriteString("### Discovery & troubleshooting\n\n")
	b.WriteString("- `ycode shell --manifest` — JSON capability catalog (built-ins + skills + sentinels + hint patterns).\n")
	b.WriteString("- `ycode shell --suggest \"<cmd>\"` — emit hints for a command without executing it.\n")
	b.WriteString("- If an MCP tool returns *connection refused*, run `ycode serve` first; live endpoints are advertised in `~/.agents/ycode/manifest.json`.\n")
	b.WriteString("- See `docs/shell-agent.md` in the ycode repo for full integration recipes.")
	return b.String()
}

// writeIfChanged writes content atomically iff different from the
// existing file. Returns (changed, error).
func writeIfChanged(path string, content []byte) (bool, error) {
	existing, _ := os.ReadFile(path)
	if bytes.Equal(existing, content) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}

// marshalStable JSON-marshals m with sorted keys at every level so the
// on-disk file is stable across runs (idempotent re-writes match the
// existing bytes; no spurious diffs).
func marshalStable(m map[string]any) ([]byte, error) {
	return marshalStableValue(m, 0)
}

func marshalStableValue(v any, indent int) ([]byte, error) {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
			writeIndent(&buf, indent+1)
			kJSON, _ := json.Marshal(k)
			buf.Write(kJSON)
			buf.WriteString(": ")
			child, err := marshalStableValue(val[k], indent+1)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		if len(keys) > 0 {
			buf.WriteByte('\n')
			writeIndent(&buf, indent)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteByte('\n')
			writeIndent(&buf, indent+1)
			child, err := marshalStableValue(item, indent+1)
			if err != nil {
				return nil, err
			}
			buf.Write(child)
		}
		if len(val) > 0 {
			buf.WriteByte('\n')
			writeIndent(&buf, indent)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		// Defer to encoding/json for primitives. json.Marshal emits
		// compact output; we leave that as-is.
		return json.Marshal(val)
	}
}

func writeIndent(buf *bytes.Buffer, level int) {
	for i := 0; i < level; i++ {
		buf.WriteString("  ")
	}
}
