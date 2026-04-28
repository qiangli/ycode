// machine.go provides machine lifecycle management for ycode.
// On macOS, containers require a Linux VM. This module manages
// machine init/start/stop so ycode can auto-provision the VM
// without the user installing or running podman separately.
package container

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	vfkitEmbed "github.com/qiangli/ycode/internal/container/vfkit_embed"
)

// MachineConfig holds configuration for the Linux VM.
type MachineConfig struct {
	Name   string // VM name (default: "ycode-default")
	CPUs   int    // number of CPUs (default: 2)
	Memory int    // memory in MB (default: 4096)
	Disk   int    // disk size in GB (default: 50)
}

// DefaultMachineConfig returns sensible defaults.
func DefaultMachineConfig() MachineConfig {
	return MachineConfig{
		Name:   "ycode-default",
		CPUs:   2,
		Memory: 4096,
		Disk:   50,
	}
}

// EnsureMachine ensures a podman machine is initialized and running.
// Called automatically by NewEngine on macOS when no socket is found.
func EnsureMachine(ctx context.Context, cfg MachineConfig) error {
	// Ensure vfkit helper is available (macOS).
	ensureVfkitOnPath()

	// Find podman binary for machine management.
	podmanPath, err := discoverBinary("")
	if err != nil {
		return fmt.Errorf("podman binary needed for machine management: %w", err)
	}

	// Check if machine already exists and is running.
	if running, _ := isMachineRunning(ctx, podmanPath, cfg.Name); running {
		slog.Info("container: machine already running", "name", cfg.Name)
		return nil
	}

	// Check if machine exists but is stopped.
	if exists, _ := machineExists(ctx, podmanPath, cfg.Name); !exists {
		slog.Info("container: initializing machine (first-time setup, downloads ~800MB VM image)",
			"name", cfg.Name, "cpus", cfg.CPUs, "memory_mb", cfg.Memory, "disk_gb", cfg.Disk)
		if err := machineInit(ctx, podmanPath, cfg); err != nil {
			return fmt.Errorf("machine init: %w", err)
		}
	}

	slog.Info("container: starting machine", "name", cfg.Name)
	if err := machineStart(ctx, podmanPath, cfg.Name); err != nil {
		return fmt.Errorf("machine start: %w", err)
	}

	// Wait for socket to become available after machine starts.
	for i := 0; i < 10; i++ {
		if socketPath := defaultSocketPath(); socketPath != "" {
			if err := waitForSocket(socketPath, 15*time.Second); err == nil {
				slog.Info("container: machine ready", "name", cfg.Name, "socket", socketPath)
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("machine started but socket not available after 30s")
}

// StopMachine stops the podman machine.
func StopMachine(ctx context.Context, name string) error {
	podmanPath, err := discoverBinary("")
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, podmanPath, "machine", "stop", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("machine stop: %s", string(out))
	}
	return nil
}

// --- internal helpers ---

// ensureVfkitOnPath extracts the embedded vfkit (if available) and adds it to PATH.
func ensureVfkitOnPath() {
	if !vfkitEmbed.Available() {
		return
	}
	cacheDir := defaultBinCacheDir()
	path, err := vfkitEmbed.EnsureVfkit(cacheDir)
	if err != nil {
		slog.Warn("container: embedded vfkit extraction failed", "error", err)
		return
	}
	dir := filepath.Dir(path)
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, dir) {
		os.Setenv("PATH", dir+string(os.PathListSeparator)+currentPath)
	}
	slog.Info("container: using embedded vfkit", "path", path)
}

func isMachineRunning(ctx context.Context, podmanPath, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, podmanPath, "machine", "inspect", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), `"Running"`), nil
}

func machineExists(ctx context.Context, podmanPath, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, podmanPath, "machine", "inspect", name)
	return cmd.Run() == nil, nil
}

func machineInit(ctx context.Context, podmanPath string, cfg MachineConfig) error {
	args := []string{"machine", "init", cfg.Name,
		fmt.Sprintf("--cpus=%d", cfg.CPUs),
		fmt.Sprintf("--memory=%d", cfg.Memory),
		fmt.Sprintf("--disk-size=%d", cfg.Disk),
	}
	cmd := exec.CommandContext(ctx, podmanPath, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func machineStart(ctx context.Context, podmanPath, name string) error {
	cmd := exec.CommandContext(ctx, podmanPath, "machine", "start", name)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
