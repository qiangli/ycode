package container

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ContainerConfig holds the configuration for creating a container.
type ContainerConfig struct {
	Name     string            // container name
	Image    string            // container image
	Env      map[string]string // environment variables
	Mounts   []Mount           // volume mounts
	WorkDir  string            // working directory inside container
	Network  string            // network name (empty = default bridge)
	ReadOnly bool              // read-only root filesystem
	CapDrop  []string          // capabilities to drop (default: ["ALL"])
	Tmpfs    []string          // tmpfs mounts (e.g., /tmp, /var/tmp)
	Init     bool              // use --init for signal handling
	Labels   map[string]string // container labels for tracking
	Resources
}

// Mount describes a bind mount from host to container.
type Mount struct {
	Source   string // host path
	Target   string // container path
	ReadOnly bool   // read-only mount
}

// Resources holds resource limits for a container.
type Resources struct {
	CPUs   string // CPU limit (e.g., "2.0")
	Memory string // memory limit (e.g., "4g")
}

// Container represents a running or created container.
type Container struct {
	ID     string
	Name   string
	engine *Engine
}

// ContainerInfo holds inspection data from podman.
type ContainerInfo struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	State  string `json:"State"`
	Image  string `json:"Image"`
	Status string `json:"Status"`
}

// CreateContainer creates a new container from the given config.
func (e *Engine) CreateContainer(ctx context.Context, cfg *ContainerConfig) (*Container, error) {
	args := []string{"create"}

	if cfg.Name != "" {
		args = append(args, "--name", cfg.Name)
	}

	// Security defaults.
	if cfg.ReadOnly {
		args = append(args, "--read-only")
	}
	if cfg.Init {
		args = append(args, "--init")
	}

	capDrop := cfg.CapDrop
	if len(capDrop) == 0 {
		capDrop = []string{"ALL"} // secure default
	}
	for _, cap := range capDrop {
		args = append(args, "--cap-drop="+cap)
	}

	// tmpfs mounts for writable scratch when root is read-only.
	for _, t := range cfg.Tmpfs {
		args = append(args, "--tmpfs", t)
	}

	// Environment variables.
	for k, v := range cfg.Env {
		args = append(args, "-e", k+"="+v)
	}

	// Volume mounts.
	for _, m := range cfg.Mounts {
		mode := "rw"
		if m.ReadOnly {
			mode = "ro"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.Source, m.Target, mode))
	}

	// Working directory.
	if cfg.WorkDir != "" {
		args = append(args, "-w", cfg.WorkDir)
	}

	// Network.
	if cfg.Network != "" {
		args = append(args, "--network", cfg.Network)
	}

	// Resource limits.
	if cfg.CPUs != "" {
		args = append(args, "--cpus="+cfg.CPUs)
	}
	if cfg.Memory != "" {
		args = append(args, "--memory="+cfg.Memory)
	}

	// Labels.
	for k, v := range cfg.Labels {
		args = append(args, "--label", k+"="+v)
	}

	args = append(args, cfg.Image)

	out, err := e.Run(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	id := strings.TrimSpace(string(out))
	name := cfg.Name
	if name == "" {
		name = id[:12]
	}

	return &Container{
		ID:     id,
		Name:   name,
		engine: e,
	}, nil
}

// Start starts the container.
func (c *Container) Start(ctx context.Context) error {
	_, err := c.engine.Run(ctx, "start", c.ID)
	return err
}

// Stop gracefully stops the container with the given timeout.
func (c *Container) Stop(ctx context.Context, timeout time.Duration) error {
	_, err := c.engine.Run(ctx, "stop", "-t", fmt.Sprintf("%d", int(timeout.Seconds())), c.ID)
	return err
}

// Remove removes the container. Use force=true to remove a running container.
func (c *Container) Remove(ctx context.Context, force bool) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, c.ID)
	_, err := c.engine.Run(ctx, args...)
	return err
}

// Exec runs a command inside the container and returns the output.
func (c *Container) Exec(ctx context.Context, command string, workDir string) (*ExecResult, error) {
	args := []string{"exec"}

	if workDir != "" {
		args = append(args, "-w", workDir)
	}

	args = append(args, c.ID, "sh", "-c", command)

	out, err := c.engine.Run(ctx, args...)
	result := &ExecResult{
		Stdout: strings.TrimRight(string(out), "\n"),
	}

	if err != nil {
		// Try to extract exit code from error.
		result.ExitCode = 1
		result.Stderr = err.Error()
	}

	return result, nil
}

// ExecResult holds the result of a command execution inside a container.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// IsRunning returns true if the container is currently running.
func (c *Container) IsRunning(ctx context.Context) bool {
	out, err := c.engine.Run(ctx, "inspect", "--format", "{{.State.Running}}", c.ID)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// CopyTo copies a file from host to the container.
func (c *Container) CopyTo(ctx context.Context, hostPath, containerPath string) error {
	_, err := c.engine.Run(ctx, "cp", hostPath, c.ID+":"+containerPath)
	return err
}

// CopyFrom copies a file from the container to the host.
func (c *Container) CopyFrom(ctx context.Context, containerPath, hostPath string) error {
	_, err := c.engine.Run(ctx, "cp", c.ID+":"+containerPath, hostPath)
	return err
}

// ListContainers lists containers matching the given filters.
func (e *Engine) ListContainers(ctx context.Context, filters map[string]string) ([]ContainerInfo, error) {
	args := []string{"ps", "-a", "--no-trunc"}
	for k, v := range filters {
		args = append(args, "--filter", k+"="+v)
	}

	var containers []ContainerInfo
	if err := e.RunJSON(ctx, &containers, args...); err != nil {
		return nil, err
	}
	return containers, nil
}

// InspectContainer returns detailed information about a container.
func (e *Engine) InspectContainer(ctx context.Context, idOrName string) (json.RawMessage, error) {
	out, err := e.Run(ctx, "inspect", idOrName)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}
