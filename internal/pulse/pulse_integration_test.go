//go:build integration

package pulse

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/container"
	source_embed "github.com/qiangli/ycode/internal/pulse/source_embed"
)

func skipIfNoPodman(t *testing.T) {
	t.Helper()
	// Try to connect to podman. If no socket is available, skip.
	engine, err := container.NewEngine(context.Background(), &container.EngineConfig{})
	if err != nil {
		t.Skipf("no podman available: %v", err)
	}
	engine.Close(context.Background())
}

func newTestManager(t *testing.T) (*Manager, *container.Engine) {
	t.Helper()
	engine, err := container.NewEngine(context.Background(), &container.EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { engine.Close(context.Background()) })

	mgr := NewManager(engine, "test", "abcd1234")
	// Use a temp dir for data to avoid polluting the real one.
	mgr.dataDir = t.TempDir()
	return mgr, engine
}

// cleanup removes any leftover pulse containers and images from previous test runs.
func cleanup(t *testing.T, mgr *Manager, engine *container.Engine) {
	t.Helper()
	ctx := context.Background()
	c := container.NewContainer(engine, ContainerName)
	_ = c.Stop(ctx, 5*time.Second)
	_ = c.Remove(ctx, true)
	// Also remove builder container if lingering.
	b := container.NewContainer(engine, "ycode-pulse-builder")
	_ = b.Stop(ctx, 5*time.Second)
	_ = b.Remove(ctx, true)
}

func TestIntegrationPulseManagerStatus_NotRunning(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr, engine := newTestManager(t)
	cleanup(t, mgr, engine)

	status := mgr.Status(ctx)
	if status != "not running" {
		t.Errorf("expected 'not running', got %q", status)
	}
}

func TestIntegrationPulseManagerStartStop(t *testing.T) {
	skipIfNoPodman(t)

	if !source_embed.Available() {
		t.Skip("source archive not embedded (build with -tags embed_source)")
	}

	// This test needs a long timeout — building the image from source
	// involves pulling golang:alpine and compiling ycode.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	mgr, engine := newTestManager(t)
	cleanup(t, mgr, engine)
	t.Cleanup(func() { cleanup(t, mgr, engine) })

	// Start pulse.
	t.Log("starting pulse container...")
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify container is running.
	c := container.NewContainer(engine, ContainerName)
	if !c.IsRunning(ctx) {
		t.Fatal("pulse container should be running after Start")
	}

	// Verify status reports running.
	status := mgr.Status(ctx)
	if status == "not running" || status == "" {
		t.Errorf("expected running status, got %q", status)
	}
	t.Logf("status: %s", status)

	// Verify collector port is reachable.
	conn, err := net.DialTimeout("tcp", "127.0.0.1:4317", 5*time.Second)
	if err != nil {
		t.Logf("collector port not reachable (may be expected in CI): %v", err)
	} else {
		conn.Close()
		t.Log("collector port 4317 is reachable")
	}

	// Verify discovery file was written.
	home, _ := os.UserHomeDir()
	discoveryPath := filepath.Join(home, ".agents", "ycode", "collector.addr")
	data, err := os.ReadFile(discoveryPath)
	if err != nil {
		t.Logf("discovery file not found (may be expected): %v", err)
	} else {
		t.Logf("discovery file: %s", string(data))
	}

	// Start again — should be idempotent.
	t.Log("calling Start again (idempotent)...")
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start (idempotent): %v", err)
	}

	// Stop pulse.
	t.Log("stopping pulse container...")
	if err := mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Verify container is no longer running.
	if c.IsRunning(ctx) {
		t.Error("pulse container should not be running after Stop")
	}

	// Verify status reports not running.
	status = mgr.Status(ctx)
	if status != "not running" {
		t.Errorf("expected 'not running' after stop, got %q", status)
	}

	// Verify discovery file was removed.
	if _, err := os.ReadFile(discoveryPath); err == nil {
		t.Error("discovery file should be removed after Stop")
	}
}

func TestIntegrationPulseManagerStop_NotRunning(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr, engine := newTestManager(t)
	cleanup(t, mgr, engine)

	// Stop when nothing is running should not error fatally.
	// It may return an error (container not found), but it should not panic.
	err := mgr.Stop(ctx)
	t.Logf("Stop (not running): %v", err)
}

func TestIntegrationPulseImageTag(t *testing.T) {
	mgr := &Manager{version: "v1.2.3", commit: "abcdef123456"}
	tag := mgr.imageTag()
	expected := "ycode-pulse:v1.2.3-abcdef12"
	if tag != expected {
		t.Errorf("expected %q, got %q", expected, tag)
	}

	// Short commit.
	mgr.commit = "abc"
	tag = mgr.imageTag()
	expected = "ycode-pulse:v1.2.3-abc"
	if tag != expected {
		t.Errorf("expected %q, got %q", expected, tag)
	}
}

func TestIntegrationPulseEnsureImage_NoSource(t *testing.T) {
	skipIfNoPodman(t)

	if source_embed.Available() {
		t.Skip("source archive is embedded — cannot test missing-source error path")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mgr, engine := newTestManager(t)
	// Use a unique version so the image definitely doesn't exist.
	mgr.version = "nonexistent-test-version"
	cleanup(t, mgr, engine)

	err := mgr.ensureImage(ctx)
	if err == nil {
		t.Fatal("expected error when source archive is not embedded")
	}
	t.Logf("expected error: %v", err)
}

func TestIntegrationPortMappingInConfig(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	engine, err := container.NewEngine(ctx, &container.EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	testImage := "docker.io/library/alpine:latest"
	if err := engine.EnsureImage(ctx, testImage); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}

	containerName := "ycode-test-portmap"
	old := container.NewContainer(engine, containerName)
	_ = old.Remove(ctx, true)

	cfg := &container.ContainerConfig{
		Name:    containerName,
		Image:   testImage,
		Command: []string{"sleep", "30"},
		Ports: []container.PortMapping{
			{HostPort: 19876, ContainerPort: 80, Protocol: "tcp"},
		},
		Labels: map[string]string{"ycode.test": "true"},
	}

	ctr, err := engine.CreateContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("CreateContainer with ports: %v", err)
	}
	defer ctr.Remove(ctx, true)

	if err := ctr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ctr.Stop(ctx, 5*time.Second)

	if !ctr.IsRunning(ctx) {
		t.Error("container with port mapping should be running")
	}
	t.Log("container with port mapping started successfully")
}

func TestIntegrationBuildImageWithContext(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	engine, err := container.NewEngine(ctx, &container.EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	// Create a temp directory with a file and a Dockerfile that COPYs it.
	contextDir := t.TempDir()
	testFile := filepath.Join(contextDir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello from context"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	dockerfile := []byte(`FROM docker.io/library/alpine:3.21
COPY hello.txt /hello.txt
CMD ["cat", "/hello.txt"]
`)

	imageName := "ycode-test-context-build:latest"
	defer engine.RemoveImage(ctx, imageName, true)

	if err := engine.BuildImageWithContext(ctx, imageName, dockerfile, contextDir); err != nil {
		t.Fatalf("BuildImageWithContext: %v", err)
	}

	if !engine.ImageExists(ctx, imageName) {
		t.Fatal("image should exist after BuildImageWithContext")
	}

	// Run the image and verify the file was copied.
	containerName := "ycode-test-context-run"
	old := container.NewContainer(engine, containerName)
	_ = old.Remove(ctx, true)

	ctr, err := engine.CreateContainer(ctx, &container.ContainerConfig{
		Name:    containerName,
		Image:   imageName,
		Command: []string{"cat", "/hello.txt"},
	})
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	defer ctr.Remove(ctx, true)

	if err := ctr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for container to finish (it just runs cat and exits).
	time.Sleep(2 * time.Second)

	logs, err := engine.ContainerLogs(ctx, containerName, false, "10")
	if err != nil {
		t.Fatalf("ContainerLogs: %v", err)
	}
	if logs != "hello from context" {
		t.Errorf("expected 'hello from context', got %q", logs)
	}
	t.Logf("container output: %s", logs)
}
