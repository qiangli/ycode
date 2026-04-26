//go:build integration

package container

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func skipIfNoPodman(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available, skipping integration test")
	}
}

func TestIntegrationEngineConnect(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx, &EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	if !engine.Healthy() {
		t.Error("engine should be healthy after init")
	}

	version, err := engine.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version == "" {
		t.Error("expected non-empty version")
	}
	t.Logf("podman version: %s", version)
}

func TestIntegrationImageOperations(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx, &EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	// Pull a tiny image.
	testImage := "docker.io/library/alpine:latest"
	if err := engine.PullImage(ctx, testImage); err != nil {
		t.Fatalf("PullImage: %v", err)
	}

	// Verify it exists.
	if !engine.ImageExists(ctx, testImage) {
		t.Error("image should exist after pull")
	}

	// List images.
	images, err := engine.ListImages(ctx)
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(images) == 0 {
		t.Error("expected at least one image")
	}
	t.Logf("found %d images", len(images))
}

func TestIntegrationContainerLifecycle(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx, &EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	// Ensure image exists.
	testImage := "docker.io/library/alpine:latest"
	if err := engine.EnsureImage(ctx, testImage); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}

	// Create container.
	containerName := "ycode-test-lifecycle"
	cfg := &ContainerConfig{
		Name:    containerName,
		Image:   testImage,
		Init:    true,
		CapDrop: []string{"ALL"},
		Labels: map[string]string{
			SessionLabel: "integration-test",
		},
	}

	// Remove any leftover from previous run.
	engine.Run(ctx, "rm", "-f", containerName)

	ctr, err := engine.CreateContainer(ctx, cfg)
	if err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}
	defer ctr.Remove(ctx, true)

	if ctr.ID == "" {
		t.Error("expected non-empty container ID")
	}

	// Start container with a long-running command.
	// We need to recreate with a command since podman create without one exits immediately.
	ctr.Remove(ctx, true)

	// Create with sleep command — labels must come before image name.
	args := []string{"create", "--name", containerName, "--init"}
	for k, v := range cfg.Labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, testImage, "sleep", "300")
	out, err := engine.Run(ctx, args...)
	if err != nil {
		t.Fatalf("create with command: %v", err)
	}
	ctr = &Container{
		ID:     strings.TrimSpace(string(out)),
		Name:   containerName,
		engine: engine,
	}
	defer ctr.Remove(ctx, true)

	if err := ctr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Check running.
	if !ctr.IsRunning(ctx) {
		t.Error("container should be running")
	}

	// Exec a command.
	result, err := ctr.Exec(ctx, "echo hello-from-container", "")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(result.Stdout, "hello-from-container") {
		t.Errorf("unexpected exec output: %q", result.Stdout)
	}

	// Stop container.
	if err := ctr.Stop(ctx, 5*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Should no longer be running.
	if ctr.IsRunning(ctx) {
		t.Error("container should not be running after stop")
	}
}

func TestIntegrationNetworkOperations(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx, &EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	networkName := "ycode-test-network"

	// Clean up first.
	engine.RemoveNetwork(ctx, networkName)

	// Create network.
	if err := engine.CreateNetwork(ctx, networkName); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	defer engine.RemoveNetwork(ctx, networkName)

	// List networks.
	networks, err := engine.ListNetworks(ctx, networkName)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}

	found := false
	for _, n := range networks {
		if n.Name == networkName {
			found = true
			break
		}
	}
	if !found {
		t.Error("created network not found in list")
	}

	// Creating the same network again should not error (idempotent).
	if err := engine.CreateNetwork(ctx, networkName); err != nil {
		t.Fatalf("CreateNetwork (idempotent): %v", err)
	}
}

func TestIntegrationContainerList(t *testing.T) {
	skipIfNoPodman(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	engine, err := NewEngine(ctx, &EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	// List all containers (just verify no error).
	containers, err := engine.ListContainers(ctx, nil)
	if err != nil {
		t.Fatalf("ListContainers: %v", err)
	}
	t.Logf("found %d containers", len(containers))
}
