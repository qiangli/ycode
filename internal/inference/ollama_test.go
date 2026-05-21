package inference

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewOllamaComponent(t *testing.T) {
	cfg := &Config{}
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
}

// Note: tests below exercise prepare() rather than Start(). The embedded
// ollama server registers handlers on http.DefaultServeMux and can only
// be started once per process (see serveOnce in ollama.go) — actually
// driving Start under `go test` would either monopolise port 11434 for
// the test binary or panic with "pattern / already registered" on the
// second test. prepare() covers the side-effects (data dir creation,
// $OLLAMA_MODELS) which is what these tests need to verify.

func TestOllamaComponent_Prepare_CreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "inference")
	comp := NewOllamaComponent(&Config{}, dir)

	if err := comp.prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("data directory was not created")
	}
}

func TestOllamaComponent_Stop_BeforeStart(t *testing.T) {
	cfg := &Config{}
	comp := NewOllamaComponent(cfg, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := comp.Stop(ctx); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
}

func TestOllamaComponent_Prepare_ModelsDir_EnvVar(t *testing.T) {
	dir := t.TempDir()
	modelsDir := filepath.Join(dir, "models")
	// Reset OLLAMA_MODELS so a leaked value from another test doesn't
	// mask a real bug in prepare's env handling.
	t.Setenv("OLLAMA_MODELS", "")
	cfg := &Config{ModelsDir: modelsDir}
	comp := NewOllamaComponent(cfg, dir)

	if err := comp.prepare(); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if got := os.Getenv("OLLAMA_MODELS"); got != modelsDir {
		t.Errorf("OLLAMA_MODELS = %q, want %q", got, modelsDir)
	}
}
