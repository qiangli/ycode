package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")

	content := `{
		"name": "test-plugin",
		"version": "1.0.0",
		"description": "A test plugin",
		"tools": [
			{
				"name": "greet",
				"description": "Says hello",
				"input_schema": {"type": "object", "properties": {"name": {"type": "string"}}},
				"command": "echo hello"
			}
		],
		"hooks": [
			{
				"event": "tool.before",
				"command": "echo before"
			}
		]
	}`

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m.Name != "test-plugin" {
		t.Errorf("expected test-plugin, got %s", m.Name)
	}
	if m.Version != "1.0.0" {
		t.Errorf("expected 1.0.0, got %s", m.Version)
	}
	if len(m.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(m.Tools))
	}
	if m.Tools[0].Name != "greet" {
		t.Errorf("expected greet, got %s", m.Tools[0].Name)
	}
	if len(m.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(m.Hooks))
	}
}

func TestLoadManifest_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	os.WriteFile(path, []byte(`{"version": "1.0.0"}`), 0o644)

	_, err := LoadManifest(path)
	if err == nil {
		t.Error("expected error for missing name")
	}
}

func TestLoadManifest_DefaultVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	os.WriteFile(path, []byte(`{"name": "test"}`), 0o644)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.0.0" {
		t.Errorf("expected default version 0.0.0, got %s", m.Version)
	}
}

func TestDiscoverManifests(t *testing.T) {
	dir := t.TempDir()

	// Create two plugin directories.
	plugin1 := filepath.Join(dir, "plugin-a")
	os.MkdirAll(plugin1, 0o755)
	os.WriteFile(filepath.Join(plugin1, "plugin.json"),
		[]byte(`{"name":"plugin-a","version":"1.0"}`), 0o644)

	plugin2 := filepath.Join(dir, "plugin-b")
	os.MkdirAll(plugin2, 0o755)
	os.WriteFile(filepath.Join(plugin2, "plugin.json"),
		[]byte(`{"name":"plugin-b","version":"2.0"}`), 0o644)

	// A directory without plugin.json should be skipped.
	os.MkdirAll(filepath.Join(dir, "not-a-plugin"), 0o755)

	plugins, err := DiscoverManifests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(plugins))
	}
}

func TestDiscoverManifests_NonexistentDir(t *testing.T) {
	plugins, err := DiscoverManifests("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins for nonexistent dir, got %d", len(plugins))
	}
}
