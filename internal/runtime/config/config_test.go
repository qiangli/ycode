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
