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

// TestOllamaComponent_Start_UseSystem verifies the escape-hatch path
// in OllamaComponent.Start: when cfg.UseSystem is true, the component
// must NOT bind its own listener and must NOT call into the embedded
// runner_embed package. It either succeeds (system daemon reachable)
// and reports BaseURL pointing at $OLLAMA_HOST, or returns a clean
// error pointing the user at their --use-system-binaries choice.
//
// We isolate the test from a real local ollama by pointing OLLAMA_HOST
// at a deliberately-unused port.
func TestOllamaComponent_Start_UseSystem_NotReachable(t *testing.T) {
	// Pick a port nothing is listening on (port 1 is reserved by IANA
	// for tcpmux; almost never bound by anything in user-space).
	t.Setenv("OLLAMA_HOST", "127.0.0.1:1")

	tr := true
	cfg := &Config{UseSystem: &tr}
	comp := NewOllamaComponent(cfg, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := comp.Start(ctx)
	if err == nil {
		t.Fatal("Start should fail when useSystem=true and no daemon is reachable")
	}
	// Verify the error mentions the user's choice rather than the
	// generic "connection refused" — surfacing the right remediation.
	msg := err.Error()
	for _, want := range []string{"system mode", "--use-system-binaries"} {
		if !contains(msg, want) {
			t.Errorf("error message should mention %q for clarity; got: %v", want, err)
		}
	}
	if comp.Healthy() {
		t.Error("component should not be healthy after failed start")
	}
}

func TestOllamaComponent_BaseURL_UseSystem(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "127.0.0.1:11434")
	tr := true
	cfg := &Config{UseSystem: &tr}
	comp := NewOllamaComponent(cfg, t.TempDir())

	// Even though Start was never called (no listener bound), BaseURL
	// must report the system URL so downstream proxies/clients have a
	// consistent endpoint regardless of which daemon is serving it.
	if got := comp.BaseURL(); got == "" {
		t.Error("BaseURL should return system URL in useSystem mode, even before Start")
	}
}

// contains is a local helper to avoid pulling in strings just for this
// test file (matches the pattern used elsewhere in the codebase).
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
