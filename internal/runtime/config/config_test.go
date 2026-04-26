package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Model == "" {
		t.Error("default model should not be empty")
	}
	if cfg.MaxTokens <= 0 {
		t.Error("default max tokens should be positive")
	}
	if cfg.PermissionMode == "" {
		t.Error("default permission mode should not be empty")
	}
}

func TestConfigGetSet(t *testing.T) {
	cfg := DefaultConfig()

	// Test Get.
	model, ok := cfg.Get("model")
	if !ok || model != cfg.Model {
		t.Errorf("Get model: got %v, %v", model, ok)
	}

	// Test Set.
	cfg.Set("model", "test-model")
	model, _ = cfg.Get("model")
	if model != "test-model" {
		t.Errorf("after Set model: got %v", model)
	}

	// Test custom key.
	cfg.Set("customKey", "customValue")
	val, ok := cfg.Get("customKey")
	if !ok || val != "customValue" {
		t.Errorf("custom key: got %v, %v", val, ok)
	}
}

func TestLoaderMerge(t *testing.T) {
	dir := t.TempDir()

	// Create user config.
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"model": "user-model", "maxTokens": 1000}`), 0o644)

	// Create project config (overrides user).
	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "settings.json"),
		[]byte(`{"model": "project-model"}`), 0o644)

	loader := NewLoader(userDir, projDir, projDir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Model != "project-model" {
		t.Errorf("expected project-model, got %q", cfg.Model)
	}
	if cfg.MaxTokens != 1000 {
		t.Errorf("expected 1000 max tokens, got %d", cfg.MaxTokens)
	}
}

func TestLoaderMergeAliases(t *testing.T) {
	dir := t.TempDir()

	// User config defines two aliases.
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"aliases": {"fast": "haiku", "smart": "opus"}}`), 0o644)

	// Project config overrides "fast" and adds "local".
	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "settings.json"),
		[]byte(`{"aliases": {"fast": "sonnet", "local": "llama3"}}`), 0o644)

	loader := NewLoader(userDir, projDir, projDir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Aliases == nil {
		t.Fatal("expected non-nil aliases")
	}
	// "fast" should be overridden by project.
	if cfg.Aliases["fast"] != "sonnet" {
		t.Errorf("fast alias: got %q, want %q", cfg.Aliases["fast"], "sonnet")
	}
	// "smart" from user should survive.
	if cfg.Aliases["smart"] != "opus" {
		t.Errorf("smart alias: got %q, want %q", cfg.Aliases["smart"], "opus")
	}
	// "local" from project should be present.
	if cfg.Aliases["local"] != "llama3" {
		t.Errorf("local alias: got %q, want %q", cfg.Aliases["local"], "llama3")
	}
}

func TestLoaderLocalOverride(t *testing.T) {
	dir := t.TempDir()

	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "settings.json"),
		[]byte(`{"permissionMode": "write"}`), 0o644)
	os.WriteFile(filepath.Join(projDir, "settings.local.json"),
		[]byte(`{"permissionMode": "plan"}`), 0o644)

	loader := NewLoader(dir, projDir, projDir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.PermissionMode != "plan" {
		t.Errorf("expected plan from settings.local.json, got %q", cfg.PermissionMode)
	}
}

func TestGetSetLocalConfigField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.local.json")

	// Get from non-existent file.
	_, ok := GetLocalConfigField(path, "model")
	if ok {
		t.Error("expected not found for non-existent file")
	}

	// Set a field.
	if err := SetLocalConfigField(path, "model", "test-model"); err != nil {
		t.Fatal(err)
	}

	// Get it back.
	val, ok := GetLocalConfigField(path, "model")
	if !ok || val != "test-model" {
		t.Errorf("expected test-model, got %v", val)
	}

	// Set another field — first field should survive.
	if err := SetLocalConfigField(path, "permissionMode", "plan"); err != nil {
		t.Fatal(err)
	}
	val, ok = GetLocalConfigField(path, "model")
	if !ok || val != "test-model" {
		t.Errorf("model should survive, got %v", val)
	}

	// Remove a field.
	if err := SetLocalConfigField(path, "permissionMode", nil); err != nil {
		t.Fatal(err)
	}
	_, ok = GetLocalConfigField(path, "permissionMode")
	if ok {
		t.Error("expected permissionMode to be removed")
	}
}

func TestLoaderMergeInstructions(t *testing.T) {
	dir := t.TempDir()

	// User config: one instruction path.
	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"instructions": ["~/global/AGENTS.md"]}`), 0o644)

	// Project config: another instruction path — should concatenate, not replace.
	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "settings.json"),
		[]byte(`{"instructions": ["docs/INSTRUCTIONS.md"]}`), 0o644)

	// Use a separate localDir with no settings to avoid double-loading projDir.
	localDir := filepath.Join(dir, "local")
	os.MkdirAll(localDir, 0o755)

	loader := NewLoader(userDir, projDir, localDir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(cfg.Instructions) != 2 {
		t.Fatalf("expected 2 instructions (concatenated), got %d: %v", len(cfg.Instructions), cfg.Instructions)
	}
	if cfg.Instructions[0] != "~/global/AGENTS.md" {
		t.Errorf("first instruction: got %q", cfg.Instructions[0])
	}
	if cfg.Instructions[1] != "docs/INSTRUCTIONS.md" {
		t.Errorf("second instruction: got %q", cfg.Instructions[1])
	}
}

func TestLoaderMergeContainerConfig(t *testing.T) {
	dir := t.TempDir()

	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"container": {"enabled": true, "image": "ycode-sandbox:latest", "cpus": "2.0"}}`), 0o644)

	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "settings.json"),
		[]byte(`{"container": {"memory": "4g", "poolSize": 3, "readOnlyRoot": true}}`), 0o644)

	localDir := filepath.Join(dir, "local")
	os.MkdirAll(localDir, 0o755)

	loader := NewLoader(userDir, projDir, localDir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.Container == nil {
		t.Fatal("expected non-nil Container config")
	}
	if !cfg.Container.Enabled {
		t.Error("expected container enabled from user config")
	}
	if cfg.Container.Image != "ycode-sandbox:latest" {
		t.Errorf("unexpected image: %s", cfg.Container.Image)
	}
	if cfg.Container.CPUs != "2.0" {
		t.Errorf("unexpected cpus: %s", cfg.Container.CPUs)
	}
	if cfg.Container.Memory != "4g" {
		t.Errorf("unexpected memory from project: %s", cfg.Container.Memory)
	}
	if cfg.Container.PoolSize != 3 {
		t.Errorf("unexpected pool size: %d", cfg.Container.PoolSize)
	}
	if !cfg.Container.ReadOnlyRoot {
		t.Error("expected read-only root from project config")
	}
}

func TestLoaderMergeGitServerConfig(t *testing.T) {
	dir := t.TempDir()

	userDir := filepath.Join(dir, "user")
	os.MkdirAll(userDir, 0o755)
	os.WriteFile(filepath.Join(userDir, "settings.json"),
		[]byte(`{"gitServer": {"enabled": true, "appName": "My Git"}}`), 0o644)

	projDir := filepath.Join(dir, "project")
	os.MkdirAll(projDir, 0o755)
	os.WriteFile(filepath.Join(projDir, "settings.json"),
		[]byte(`{"gitServer": {"httpOnly": true, "dataDir": "/data/gitea"}}`), 0o644)

	localDir := filepath.Join(dir, "local")
	os.MkdirAll(localDir, 0o755)

	loader := NewLoader(userDir, projDir, localDir)
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if cfg.GitServer == nil {
		t.Fatal("expected non-nil GitServer config")
	}
	if !cfg.GitServer.Enabled {
		t.Error("expected gitServer enabled")
	}
	if cfg.GitServer.AppName != "My Git" {
		t.Errorf("unexpected app name: %s", cfg.GitServer.AppName)
	}
	if !cfg.GitServer.HTTPOnly {
		t.Error("expected httpOnly from project config")
	}
	if cfg.GitServer.DataDir != "/data/gitea" {
		t.Errorf("unexpected data dir: %s", cfg.GitServer.DataDir)
	}
}

func TestLoaderMissingFiles(t *testing.T) {
	loader := NewLoader("/nonexistent/user", "/nonexistent/project", "/nonexistent/local")
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("load should not error on missing files: %v", err)
	}
	if cfg.Model == "" {
		t.Error("should have default model")
	}
}
