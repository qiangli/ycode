package selfinit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// opencodeTool is the Tool implementation for sst/opencode.
type opencodeTool struct{}

func init() {
	RegisterTool(&opencodeTool{})
}

func (o *opencodeTool) Name() string { return "opencode" }

// Detect reports whether OpenCode looks installed. True if `opencode`
// is on PATH or `~/.config/opencode/` exists.
func (o *opencodeTool) Detect() bool {
	if _, err := exec.LookPath("opencode"); err == nil {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode")); err == nil {
		return true
	}
	return false
}

// opencodeUserConfigPath returns the path to the user-scope opencode
// config (opencode.json under XDG_CONFIG_HOME).
func opencodeUserConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "opencode.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}

// opencodeUserMemoryPath returns the user-scope AGENTS.md OpenCode
// reads as system context.
func opencodeUserMemoryPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "AGENTS.md"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode", "AGENTS.md"), nil
}

// WriteMCP merges ycode capability entries into opencode.json's "mcp"
// map. OpenCode's schema differs from Claude's:
//
//	{
//	  "mcp": {
//	    "ycode-loom": {"type": "remote", "url": "http://...", "enabled": true},
//	    "ycode-stdio": {"type": "local", "command": ["ycode", "mcp", "serve"], "enabled": true}
//	  }
//	}
//
// "type" is local|remote (not stdio|http) and command is a single
// flattened list (binary + args), not split.
func (o *opencodeTool) WriteMCP(_ context.Context, caps []CapabilitySpec) (bool, error) {
	path, err := opencodeUserConfigPath()
	if err != nil {
		return false, err
	}

	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &root)
	}

	servers, _ := root["mcp"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	desired := map[string]bool{}
	for _, cs := range caps {
		desired[cs.Name] = true
	}
	for name := range servers {
		if strings.HasPrefix(name, "ycode-") && !desired[name] {
			delete(servers, name)
		}
	}

	for _, cs := range caps {
		entry := map[string]any{"enabled": true}
		switch cs.Transport {
		case "stdio":
			cmd := append([]string{cs.Command}, cs.Args...)
			cmdAny := make([]any, len(cmd))
			for i, s := range cmd {
				cmdAny[i] = s
			}
			entry["type"] = "local"
			entry["command"] = cmdAny
		case "http":
			entry["type"] = "remote"
			entry["url"] = cs.URL
		default:
			continue
		}
		servers[cs.Name] = entry
	}
	root["mcp"] = servers

	out, err := marshalStable(root)
	if err != nil {
		return false, err
	}
	return writeIfChanged(path, out)
}

// WriteInstructions splices the L2 awareness block into OpenCode's
// user-scope AGENTS.md.
func (o *opencodeTool) WriteInstructions(_ context.Context, caps []CapabilitySpec) (bool, error) {
	path, err := opencodeUserMemoryPath()
	if err != nil {
		return false, err
	}
	body := buildInstructionsBlock(caps)
	existing, _ := os.ReadFile(path)
	new := SpliceBlock(string(existing), body)
	return writeIfChanged(path, []byte(new))
}
