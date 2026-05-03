// machine.go provides machine lifecycle management for ycode.
// On macOS/Windows, containers require a Linux VM. This module manages
// machine init/start/stop using podman's Go libraries directly —
// no external podman binary needed.
package container

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	ociMachine "github.com/qiangli/ycode/pkg/oci/machine"

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
// Uses podman's Go libraries directly — no external binary needed.
func EnsureMachine(ctx context.Context, cfg MachineConfig) error {
	// Ensure vfkit helper is available (macOS VM hypervisor).
	ensureVfkitOnPath()

	// Get the platform's VM provider (AppleHV on macOS, QEMU on Linux, HyperV on Windows).
	mp, err := ociMachine.GetProvider()
	if err != nil {
		return fmt.Errorf("get machine provider: %w", err)
	}

	// Check if machine already exists and is running.
	mc, exists := findMachine(cfg.Name, mp)
	if exists {
		state, err := mp.State(mc, false)
		if err == nil && state == ociMachine.Running {
			slog.Info("container: machine already running", "name", cfg.Name)
			return nil
		}

		// Machine exists but not running — start it.
		slog.Info("container: starting machine", "name", cfg.Name)
		updateConn := true
		if err := ociMachine.Start(mc, mp, ociMachine.StartOptions{}, &updateConn); err != nil {
			return fmt.Errorf("machine start: %w", err)
		}
	} else {
		// Machine doesn't exist — init and start.
		slog.Info("container: initializing machine (first-time setup, downloads ~800MB VM image)",
			"name", cfg.Name, "cpus", cfg.CPUs, "memory_mb", cfg.Memory, "disk_gb", cfg.Disk)

		initOpts := ociMachine.InitOptions{
			Name:      cfg.Name,
			CPUS:      uint64(cfg.CPUs),
			Memory:    uint64(cfg.Memory),
			DiskSize:  uint64(cfg.Disk),
			IsDefault: true,
		}

		if err := ociMachine.Init(initOpts, mp); err != nil {
			return fmt.Errorf("machine init: %w", err)
		}

		// Re-find the machine config after init.
		mc, exists = findMachine(cfg.Name, mp)
		if !exists {
			return fmt.Errorf("machine init succeeded but config not found")
		}

		slog.Info("container: starting machine", "name", cfg.Name)
		updateConn := true
		if err := ociMachine.Start(mc, mp, ociMachine.StartOptions{}, &updateConn); err != nil {
			return fmt.Errorf("machine start: %w", err)
		}
	}

	// Wait for socket to become available.
	for i := 0; i < 15; i++ {
		if socketPath := defaultSocketPath(); socketPath != "" {
			if err := waitForSocket(socketPath, 2*time.Second); err == nil {
				slog.Info("container: machine ready", "name", cfg.Name, "socket", socketPath)
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("machine started but socket not available after 30s")
}

// StopMachine stops the podman machine using Go libraries.
func StopMachine(ctx context.Context, name string) error {
	mp, err := ociMachine.GetProvider()
	if err != nil {
		return err
	}

	mc, exists := findMachine(name, mp)
	if !exists {
		return fmt.Errorf("machine %q not found", name)
	}

	return ociMachine.Stop(mc, mp, false)
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

// findMachine looks up a machine config by name from the provider.
func findMachine(name string, mp ociMachine.VMProvider) (*ociMachine.MachineConfig, bool) {
	dirs, err := ociMachine.GetMachineDirs(mp.VMType())
	if err != nil {
		return nil, false
	}
	mc, err := ociMachine.LoadMachineByName(name, dirs)
	if err != nil {
		return nil, false
	}
	return mc, true
}
