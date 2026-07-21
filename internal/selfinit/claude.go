package selfinit

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

// claudeUserMemoryPath returns the path Claude Code reads at user
// scope for context/memory injection.
func claudeUserMemoryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "CLAUDE.md"), nil
}

// WriteInstructions splices the L2 awareness block into Claude Code's
// user-scope memory file (~/.claude/CLAUDE.md).
func (c *claude) WriteInstructions(_ context.Context) (bool, error) {
	path, err := claudeUserMemoryPath()
	if err != nil {
		return false, err
	}
	body := buildInstructionsBlock()
	existing, _ := os.ReadFile(path)
	new := SpliceBlock(string(existing), body)
	return writeIfChanged(path, []byte(new))
}

// buildInstructionsBlock constructs the L2 content that lands inside
// <BEGIN/END YCODE> in foreign tools' memory files.
//
// One surface only: the **`yc <verb>` shell built-ins** — bash-callable
// commands active in two scenarios:
//
//   - Foreign tool's bash backend points at `ycode shell -c` (or a PATH
//     wrapper that does — see docs/shell-agent.md).
//   - The user types `ycode shell -c "yc <verb> ..."` manually.
//
// ycode used to advertise MCP servers here too. It no longer runs any
// (docs/plan-remove-mcp.md), and naming a server the binary doesn't
// serve made every foreign CLI report a failed connection at startup.
// The symbol/graph capabilities that block described live in the `yc`
// verbs below — same Go code, reachable as plain bash.
func buildInstructionsBlock() string {
	var b strings.Builder
	b.WriteString("## ycode capabilities\n\n")
	b.WriteString("ycode runs locally and exposes its capabilities as bash-callable shell built-ins. Prefer them over generic shell tools when the language/use-case matches. ycode does not run an MCP server — do not configure one.\n\n")

	b.WriteString("### `yc <verb>` shell built-ins\n\n")
	b.WriteString("Active whenever your bash backend routes through `ycode shell -c` (e.g., a PATH wrapper at `~/bin/ycode-wrappers/bash`, or direct `ycode shell -c \"...\"` invocation). Use them by default — the agent-mode hint engine surfaces suggestions on stderr.\n\n")
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
	b.WriteString("- If a verb that needs the server returns *connection refused*, run `ycode serve` first; live endpoints are advertised in `~/.agents/ycode/manifest.json`.\n")
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
