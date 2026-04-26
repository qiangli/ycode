package inference

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewOllamaComponent(t *testing.T) {
	cfg := &Config{Enabled: true}
	comp := NewOllamaComponent(cfg, "/tmp/test-data")

	if comp.Name() != "ollama" {
		t.Errorf("Name() = %q, want %q", comp.Name(), "ollama")
	}
	if comp.Healthy() {
		t.Error("new component should not be healthy")
	}
	if comp.Port() != 0 {
		t.Errorf("Port() = %d, want 0", comp.Port())
	}
	if comp.BaseURL() != "" {
		t.Errorf("BaseURL() = %q, want empty", comp.BaseURL())
	}
	if comp.HTTPHandler() != nil {
		t.Error("HTTPHandler() should be nil before start")
	}
	if comp.Runner() != nil {
		t.Error("Runner() should be nil before start")
	}
}

func TestOllamaComponent_Start_NoRunner(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Enabled:    true,
		RunnerPath: "/nonexistent/ollama",
	}
	comp := NewOllamaComponent(cfg, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := comp.Start(ctx)
	if err == nil {
		t.Fatal("expected error when runner not found")
	}

	if comp.Healthy() {
		t.Error("should not be healthy after failed start")
	}
}

func TestOllamaComponent_Start_CreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "inference")
	cfg := &Config{
		Enabled:    true,
		RunnerPath: "/nonexistent/ollama",
	}
	comp := NewOllamaComponent(cfg, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start will fail because runner doesn't exist, but data dir should be created.
	_ = comp.Start(ctx)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("data directory was not created")
	}
}

func TestOllamaComponent_Stop_BeforeStart(t *testing.T) {
	cfg := &Config{Enabled: true}
	comp := NewOllamaComponent(cfg, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := comp.Stop(ctx); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
}

func TestOllamaComponent_ModelsDir_EnvVar(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	cfg := &Config{
		Enabled:    true,
		ModelsDir:  modelsDir,
		RunnerPath: "/nonexistent/ollama",
	}
	comp := NewOllamaComponent(cfg, dir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start will fail, but OLLAMA_MODELS should be set.
	_ = comp.Start(ctx)

	if got := os.Getenv("OLLAMA_MODELS"); got != modelsDir {
		t.Errorf("OLLAMA_MODELS = %q, want %q", got, modelsDir)
	}
}
