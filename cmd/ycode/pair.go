package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/selfinit"
)

// pairCmd implements `ycode pair --tool <name>` — the painless on-ramp for
// foreign agentic tools (and ycode's own TUI) to reach the ycode server's
// public API. It is the generic "distro package" for whichever recognized
// client the user names; the body is data-driven by the per-tool snippet
// table below. Adding a sixth recognized tool means adding a row, not a
// branch.
//
// Design choices:
//
//   - Public API only. The command emits configuration the user pastes into
//     the third-party tool. ycode never reaches into a foreign tool's tree.
//   - Remote-safe. The snippets use ${YCODE_URL} and ${YCODE_TOKEN} env-var
//     references rather than baking in localhost — same config works whether
//     ycode is on localhost or a remote host.
//   - Token-paste flow. v1 reads (or mints if missing) the existing
//     ~/.agents/ycode/server.token. A future revision adds a true device-
//     code exchange for cross-machine pairing; the CLI surface is forward-
//     compatible.
//   - ycode's own TUI is a target, not a special case. This is the Agent OS
//     canary: if the TUI uses the same public API any third-party tool does,
//     there is no privileged in-process back-channel.
var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Print bearer token + config snippet for a foreign tool",
	Long: `Pair a foreign agentic tool with this ycode server.

Reads (or mints) the server bearer token and prints a paste-ready config
fragment for the named tool, plus the URLs the tool needs.

ycode does not run an MCP server, so nothing here goes in a tool's
mcpServers config. The token pairs a client with ycode's HTTP API; code
capabilities reach a foreign CLI through its bash backend as the
'yc <verb>' shell built-ins (see docs/shell-agent.md).

Recognized tools: opencode, codex, gemini-cli, claude-code, ycode-tui.
Passing --tool=<unrecognized-name> falls through to a generic snippet
you can adapt to the foreign tool's config schema.

The token is read from ~/.agents/ycode/server.token. If that file does
not exist, a fresh random token is generated and written there. Tokens
remain valid until manually rotated (delete the file to force a re-mint
on next run).

Example:

  $ ycode pair --tool opencode
  $ ycode pair --tool claude-code --url https://ycode.example.com
`,
	RunE: runPair,
}

var (
	pairTool string
	pairURL  string
	pairJSON bool
)

func init() {
	pairCmd.Flags().StringVar(&pairTool, "tool", "", "Foreign tool name (opencode|codex|gemini-cli|claude-code|ycode-tui)")
	pairCmd.Flags().StringVar(&pairURL, "url", fmt.Sprintf("http://127.0.0.1:%d", selfinit.DefaultPort), "Base URL of the ycode server (used in the snippet)")
	pairCmd.Flags().BoolVar(&pairJSON, "json", false, "Emit machine-readable JSON instead of human-friendly text")
	_ = pairCmd.MarkFlagRequired("tool")
	rootCmd.AddCommand(pairCmd)
}

func runPair(_ *cobra.Command, _ []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	tokenPath := filepath.Join(home, ".agents", "ycode", "server.token")
	token, err := readOrMintServerToken(tokenPath)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	url := strings.TrimRight(pairURL, "/")

	snippet, dest, ok := pairSnippet(pairTool, url, token)
	if !ok {
		// Generic fallback for unknown tools: print the URL + token + the
		// discovery endpoint. Don't error out — that would be hostile to
		// the long tail.
		snippet = genericSnippet(url, token)
		dest = "(adapt for your tool's config schema)"
	}

	if pairJSON {
		return emitPairJSON(pairTool, url, token, dest, snippet)
	}
	return emitPairText(pairTool, url, token, dest, snippet)
}

// readOrMintServerToken returns the bearer token at path, generating one if
// the file is missing. Mode 0600 — the file is sensitive; never commit it.
func readOrMintServerToken(path string) (string, error) {
	if data, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			return t, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// pairSnippet returns a paste-ready config fragment plus the destination
// hint for the recognized tool. ok=false means the tool name is not in the
// closed recognized set; caller falls back to a generic snippet.
//
// Closed recognized set per the integration plan (G11):
// opencode, codex, gemini-cli, claude-code, ycode-tui. Adding a sixth tool
// is an explicit code change that requires popularity / value justification.
//
// Every entry used to be an MCP server block pointing at <url>/mcp/. ycode
// retired MCP (docs/plan-remove-mcp.md): serve mounts no /mcp/ route, so
// those snippets configured a server that answers 404 and every tool that
// pasted one reported a failed MCP server at startup. The surviving
// integration path for a foreign CLI is the shell one — route its bash
// backend through `ycode shell -c` and the `yc <verb>` built-ins are in
// process. Pairing (token + URL) still matters for HTTP API clients, which
// is what these snippets now describe.
func pairSnippet(tool, url, token string) (snippet, dest string, ok bool) {
	switch tool {
	case "opencode", "claude-code", "codex", "gemini-cli":
		return fmt.Sprintf(`# ycode does not run an MCP server — there is nothing to add to
# %s's mcpServers config.
#
# To give %s ycode's capabilities, point its bash backend at
# ycode's shell so the `+"`yc <verb>`"+` built-ins resolve in process:
#
#   export YCODE_URL=%s
#   export YCODE_TOKEN=%s
#   ycode shell -c "yc symbols ./..."
#
# Or install a PATH wrapper that forwards bash to `+"`ycode shell -c`"+`;
# see docs/shell-agent.md. Run `+"`ycode init`"+` in the repo to write the
# capability block %s already reads.
#
# The token above is for ycode's HTTP API (%s/ycode/), not MCP.
`, tool, tool, url, token, tool, url), "your shell init (~/.zshrc or ~/.bashrc); see docs/shell-agent.md", true
	case "ycode-tui":
		// ycode's own TUI is a peer client. Same public API, same auth.
		// This is the Agent OS canary in concrete form.
		return fmt.Sprintf(`# Environment variables for ycode TUI as a client:
export YCODE_URL=%s
export YCODE_TOKEN=%s
# Then launch:
#   ycode --connect $YCODE_URL/ycode/
`, url, token), "your shell init (~/.zshrc or ~/.bashrc)", true
	}
	return "", "", false
}

func genericSnippet(url, token string) string {
	return fmt.Sprintf(`# ycode HTTP API reference (ycode runs no MCP server):
URL:     %s/ycode/
Auth:    Authorization: Bearer %s

# Discovery (no auth required) — lists every endpoint ycode actually serves:
%s/.well-known/ycode-manifest.json

# For in-process code capabilities, route the tool's bash through
# `+"`ycode shell -c`"+` and use the `+"`yc <verb>`"+` built-ins (docs/shell-agent.md).
`, url, token, url)
}

func emitPairText(tool, url, token, dest, snippet string) error {
	fmt.Printf("ycode → %s pairing\n", tool)
	fmt.Printf("==========================================\n")
	fmt.Printf("Server URL:     %s\n", url)
	fmt.Printf("Bearer token:   %s\n", token)
	fmt.Printf("Discovery:      %s/.well-known/ycode-manifest.json\n", url)
	fmt.Printf("API endpoint:   %s/ycode/\n", url)
	fmt.Printf("Place in:       %s\n", dest)
	fmt.Printf("------------------------------------------\n")
	fmt.Printf("%s\n", snippet)
	fmt.Printf("------------------------------------------\n")
	fmt.Printf("Keep the token secret. Rotate by deleting ~/.agents/ycode/server.token.\n")
	return nil
}

func emitPairJSON(tool, url, token, dest, snippet string) error {
	// Inline JSON (no struct type) — the shape is the contract; callers
	// parse selectively.
	fmt.Printf("{\n")
	fmt.Printf("  \"tool\": %q,\n", tool)
	fmt.Printf("  \"url\": %q,\n", url)
	fmt.Printf("  \"token\": %q,\n", token)
	fmt.Printf("  \"discovery\": %q,\n", url+"/.well-known/ycode-manifest.json")
	fmt.Printf("  \"api\": %q,\n", url+"/ycode/")
	fmt.Printf("  \"dest\": %q,\n", dest)
	fmt.Printf("  \"snippet\": %q\n", snippet)
	fmt.Printf("}\n")
	return nil
}
