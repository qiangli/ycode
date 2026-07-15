package selfinit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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
