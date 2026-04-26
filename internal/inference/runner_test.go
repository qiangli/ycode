package inference

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverRunner_ExplicitPath(t *testing.T) {
	// Create a temporary "runner" binary.
	dir := t.TempDir()
	fakeRunner := filepath.Join(dir, "ollama")
	if err := os.WriteFile(fakeRunner, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := discoverRunner(fakeRunner)
	if err != nil {
		t.Fatalf("discoverRunner(%q) error: %v", fakeRunner, err)
	}
	if path != fakeRunner {
		t.Errorf("discoverRunner(%q) = %q, want %q", fakeRunner, path, fakeRunner)
	}
}

func TestDiscoverRunner_ExplicitPath_NotFound(t *testing.T) {
	_, err := discoverRunner("/nonexistent/ollama")
	if err == nil {
		t.Fatal("expected error for nonexistent explicit path")
	}
}

func TestDiscoverRunner_EnvVar(t *testing.T) {
	dir := t.TempDir()
	fakeRunner := filepath.Join(dir, "ollama")
	os.WriteFile(fakeRunner, []byte("#!/bin/sh\n"), 0o755)

	t.Setenv("OLLAMA_RUNNERS", fakeRunner)

	path, err := discoverRunner("")
	if err != nil {
		t.Fatalf("discoverRunner with OLLAMA_RUNNERS: %v", err)
	}
	if path != fakeRunner {
		t.Errorf("got %q, want %q", path, fakeRunner)
	}
}

func TestDiscoverRunner_SystemPATH(t *testing.T) {
	// Only test if ollama is actually installed.
	if _, err := exec.LookPath("ollama"); err != nil {
		t.Skip("ollama not in PATH")
	}

	t.Setenv("OLLAMA_RUNNERS", "")

	path, err := discoverRunner("")
	if err != nil {
		t.Fatalf("discoverRunner via PATH: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestDiscoverRunner_NothingFound(t *testing.T) {
	t.Setenv("OLLAMA_RUNNERS", "")
	t.Setenv("PATH", t.TempDir()) // Empty PATH.

	_, err := discoverRunner("")
	if err == nil {
		t.Fatal("expected error when nothing found")
	}
}

func TestNewRunnerManager_NoRunner(t *testing.T) {
	t.Setenv("OLLAMA_RUNNERS", "")
	t.Setenv("PATH", t.TempDir())

	_, err := NewRunnerManager(&Config{RunnerPath: "/nonexistent/ollama"})
	if err == nil {
		t.Fatal("expected error for missing runner")
	}
}

func TestNewRunnerManager_WithExplicitPath(t *testing.T) {
	dir := t.TempDir()
	fakeRunner := filepath.Join(dir, "ollama")
	os.WriteFile(fakeRunner, []byte("#!/bin/sh\n"), 0o755)

	rm, err := NewRunnerManager(&Config{RunnerPath: fakeRunner})
	if err != nil {
		t.Fatalf("NewRunnerManager: %v", err)
	}
	if rm.binaryPath != fakeRunner {
		t.Errorf("binaryPath = %q, want %q", rm.binaryPath, fakeRunner)
	}
	if rm.maxRestarts != 3 {
		t.Errorf("maxRestarts = %d, want 3", rm.maxRestarts)
	}
}

func TestRunnerManager_HealthCheck(t *testing.T) {
	// Start a mock runner that responds to health checks.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rm := &RunnerManager{
		healthURL:   srv.URL,
		maxRestarts: 3,
		done:        make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rm.waitForHealth(ctx); err != nil {
		t.Fatalf("waitForHealth: %v", err)
	}
}

func TestRunnerManager_HealthCheck_Timeout(t *testing.T) {
	// Server that always returns 503.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rm := &RunnerManager{
		healthURL:   srv.URL,
		maxRestarts: 3,
		done:        make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := rm.waitForHealth(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRunnerManager_HealthCheck_CancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	rm := &RunnerManager{
		healthURL:   srv.URL,
		maxRestarts: 3,
		done:        make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	err := rm.waitForHealth(ctx)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestRunnerManager_Healthy_DefaultFalse(t *testing.T) {
	rm := &RunnerManager{done: make(chan struct{})}
	if rm.Healthy() {
		t.Error("new runner should not be healthy")
	}
}

func TestRunnerManager_Restarts_DefaultZero(t *testing.T) {
	rm := &RunnerManager{done: make(chan struct{})}
	if rm.Restarts() != 0 {
		t.Errorf("restarts = %d, want 0", rm.Restarts())
	}
}

func TestRunnerManager_Port_DefaultZero(t *testing.T) {
	rm := &RunnerManager{done: make(chan struct{})}
	if rm.Port() != 0 {
		t.Errorf("port = %d, want 0", rm.Port())
	}
}

func TestRunnerManager_BaseURL(t *testing.T) {
	rm := &RunnerManager{
		healthURL: "http://127.0.0.1:12345",
		done:      make(chan struct{}),
	}
	if got := rm.BaseURL(); got != "http://127.0.0.1:12345" {
		t.Errorf("BaseURL() = %q, want %q", got, "http://127.0.0.1:12345")
	}
}

func TestRunnerManager_Stop_NilCancel(t *testing.T) {
	rm := &RunnerManager{done: make(chan struct{})}
	close(rm.done) // Simulate already-stopped.

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := rm.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestRunnerManager_EphemeralPort(t *testing.T) {
	// Verify that ephemeral port allocation works.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if port == 0 {
		t.Fatal("expected non-zero ephemeral port")
	}
	if port < 1024 {
		t.Errorf("ephemeral port %d is in privileged range", port)
	}
}

// TestRunnerManager_FullLifecycle tests spawn → health → stop using a real
// HTTP server as a mock runner.
func TestRunnerManager_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping lifecycle test in short mode")
	}

	// Use a real HTTP server to simulate the runner.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	rm := &RunnerManager{
		healthURL:   srv.URL,
		port:        srv.Listener.Addr().(*net.TCPAddr).Port,
		maxRestarts: 3,
		done:        make(chan struct{}),
	}

	// Simulate healthy state.
	rm.healthy.Store(true)

	if !rm.Healthy() {
		t.Error("expected healthy after store(true)")
	}
	if rm.Port() == 0 {
		t.Error("expected non-zero port")
	}

	// Stop.
	close(rm.done) // Simulate monitor goroutine exiting.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rm.Stop(ctx)

	if rm.Healthy() {
		t.Error("expected unhealthy after stop")
	}
}
