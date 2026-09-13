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
// It describes only surfaces the current binary actually serves: the
// `agent.yaml`-compiled command surface and the governed Bashy shell
// boundary. ycode used to advertise MCP servers and `yc <verb>` shell
// built-ins here; both are gone (docs/plan-remove-mcp.md; the yc
// built-in registry was removed with the YAML-native harness rewrite),
// and advertising a surface the binary no longer serves made every
// foreign CLI fail or hallucinate commands.
func buildInstructionsBlock() string {
	var b strings.Builder
	b.WriteString("## ycode capabilities\n\n")
	b.WriteString("ycode is a local, YAML-native agent harness. `agent.yaml` is its only runtime configuration and policy source. It does not run an MCP server and installs no shell built-ins — do not configure an MCP entry for it, and do not invoke `yc <verb>` commands (they no longer exist).\n\n")

	b.WriteString("### Command surface\n\n")
	b.WriteString("All ordinary commands accept `--file/-f` and default to `agent.yaml`:\n\n")
	b.WriteString("- `ycode validate --file agent.yaml` — strict compile; unknown fields, unresolved references and unreachable resources are rejected.\n")
	b.WriteString("- `ycode prompt --file agent.yaml \"request\"` — submit one prompt through the compiled turn pipeline.\n")
	b.WriteString("- `ycode repl --file agent.yaml` / `ycode --file agent.yaml` — configured interactive frontends.\n")
	b.WriteString("- `ycode serve --file agent.yaml` — declared HTTP/WebSocket/NATS frontends only.\n")
	b.WriteString("- `ycode acp --config agent.yaml` — Agent Client Protocol over stdio.\n")
	b.WriteString("- `ycode shell --file agent.yaml -c '<cmd>'` — run one command through the governed Bashy boundary: preflight, YAML policy evaluation, digest-bound execution. There is no host-shell fallback and no permission-bypass flag.\n")
	b.WriteString("- `ycode config|model|tools|memory|skill --file agent.yaml ...` — read-only views of the compiled document.\n\n")
	b.WriteString("`ycode --help` is authoritative. Models driven by ycode see exactly one tool: `bashy`.")
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
