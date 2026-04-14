package plugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/tools"
)

func TestLoader_LoadAll(t *testing.T) {
	dir := t.TempDir()

	// Create a plugin with one tool.
	pluginDir := filepath.Join(dir, "hello-plugin")
	os.MkdirAll(pluginDir, 0o755)
	os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
		"name": "hello",
		"version": "1.0.0",
		"tools": [
			{
				"name": "greet",
				"description": "Says hello",
				"input_schema": {"type": "object"},
				"command": "echo hello"
			}
		]
	}`), 0o644)

	reg := tools.NewRegistry()
	loader := NewLoader(reg, nil)
	count, warnings := loader.LoadAll(dir)

	if count != 1 {
		t.Errorf("expected 1 tool registered, got %d", count)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	// Verify tool is registered with namespaced name.
	if _, ok := reg.Get("hello.greet"); !ok {
		t.Error("expected hello.greet to be registered")
	}
}

func TestLoader_DuplicateTool(t *testing.T) {
	dir := t.TempDir()

	// Two plugins with same tool name.
	for _, name := range []string{"a", "b"} {
		pluginDir := filepath.Join(dir, name)
		os.MkdirAll(pluginDir, 0o755)
		os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
			"name": "`+name+`",
			"version": "1.0.0",
			"tools": [{"name": "do", "description": "do it", "input_schema": {}, "command": "echo"}]
		}`), 0o644)
	}

	reg := tools.NewRegistry()
	loader := NewLoader(reg, nil)
	count, _ := loader.LoadAll(dir)

	// Both should succeed since names are namespaced: a.do and b.do
	if count != 2 {
		t.Errorf("expected 2 tools, got %d", count)
	}
}
