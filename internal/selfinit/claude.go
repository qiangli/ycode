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
func buildInstructionsBlock(caps []CapabilitySpec) string {
	var b strings.Builder
	b.WriteString("## ycode capabilities\n\n")
	b.WriteString("ycode runs locally and exposes services over MCP. Prefer them in these situations:\n\n")
	for _, cs := range caps {
		fmt.Fprintf(&b, "- **`%s`** — %s\n", cs.Name, FamilyDescription(cs.Family))
	}
	b.WriteString("\nIf a tool returns *connection refused*, run `ycode serve` first; capabilities are advertised in `~/.agents/ycode/manifest.json`.")
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
