package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
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

Recognized tools: opencode, codex, gemini-cli, claude-code, ycode-tui.
Pass --target=<other> to emit a generic snippet you can adapt.

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
	pairCmd.Flags().StringVar(&pairURL, "url", "http://127.0.0.1:58080", "Base URL of the ycode server (used in the snippet)")
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
		// Generic fallback for unknown tools: print the URL + token + a
		// boilerplate MCP block that most clients can adapt. Don't error
		// out — that would be hostile to the long tail.
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
func pairSnippet(tool, url, token string) (snippet, dest string, ok bool) {
	switch tool {
	case "opencode":
		return fmt.Sprintf(`{
  "mcp": {
    "ycode": {
      "type": "remote",
      "url": "%s/mcp/",
      "headers": { "Authorization": "Bearer %s" },
      "timeout": 30000
    }
  }
}`, url, token), "~/.opencode/opencode.jsonc", true
	case "claude-code":
		return fmt.Sprintf(`{
  "mcpServers": {
    "ycode": {
      "type": "http",
      "url": "%s/mcp/",
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}`, url, token), "project .mcp.json (recommended) or ~/.claude/settings.json under mcpServers", true
	case "codex":
		return fmt.Sprintf(`# Add to your codex config (~/.codex/config.toml):
[mcp_servers.ycode]
type = "http"
url = "%s/mcp/"
headers = { Authorization = "Bearer %s" }
`, url, token), "~/.codex/config.toml", true
	case "gemini-cli":
		return fmt.Sprintf(`{
  "mcpServers": {
    "ycode": {
      "httpUrl": "%s/mcp/",
      "headers": { "Authorization": "Bearer %s" }
    }
  }
}`, url, token), "~/.gemini/settings.json", true
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
	return fmt.Sprintf(`# Generic MCP server reference:
URL:     %s/mcp/
Auth:    Authorization: Bearer %s
Method:  POST (JSON-RPC body)

# Discovery (no auth required):
%s/.well-known/ycode-manifest.json
`, url, token, url)
}

func emitPairText(tool, url, token, dest, snippet string) error {
	fmt.Printf("ycode → %s pairing\n", tool)
	fmt.Printf("==========================================\n")
	fmt.Printf("Server URL:     %s\n", url)
	fmt.Printf("Bearer token:   %s\n", token)
	fmt.Printf("Discovery:      %s/.well-known/ycode-manifest.json\n", url)
	fmt.Printf("MCP endpoint:   %s/mcp/\n", url)
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
	fmt.Printf("  \"mcp\": %q,\n", url+"/mcp/")
	fmt.Printf("  \"dest\": %q,\n", dest)
	fmt.Printf("  \"snippet\": %q\n", snippet)
	fmt.Printf("}\n")
	return nil
}
