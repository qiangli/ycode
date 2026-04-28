// Package containertool provides a standardized pattern for running tools
// inside containers using ycode's embedded Podman engine. No external
// container runtime binary (podman/docker) is needed.
//
// Usage:
//
//	tool := &containertool.Tool{
//	    Name:       "my-tool",
//	    Image:      "ycode-my-tool:latest",
//	    Dockerfile: myDockerfile,
//	    Sources:    map[string]string{"main.go": mainSource, "go.mod": goMod},
//	    Mounts:     []containertool.Mount{{Source: workDir, Target: "/workspace", ReadOnly: true}},
//	    Engine:     engine,
//	}
//	output, err := tool.Run(ctx, inputJSON)
package containertool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/container"
)

// Tool defines a containerized tool that can be built and invoked
// via the embedded Podman engine (REST API, no external binary).
type Tool struct {
	// Name is a human-readable name for logging.
	Name string

	// Image is the container image tag (e.g., "ycode-treesitter:latest").
	Image string

	// Dockerfile is the Dockerfile content for building the image.
	Dockerfile string

	// Sources are the files needed to build the image.
	// Key is the filename (e.g., "main.go"), value is the file content.
	Sources map[string]string

	// Mounts are bind mounts for the container at runtime.
	Mounts []Mount

	// BuildTimeout is the maximum time for image build (default: 5 minutes).
	BuildTimeout time.Duration

	// RunTimeout is the maximum time for tool execution (default: 2 minutes).
	RunTimeout time.Duration

	// Engine is the container engine (required). All operations use its REST API.
	Engine *container.Engine

	// mu protects the image build to ensure it only happens once.
	mu       sync.Mutex
	built    bool
	buildErr error
}

// Mount describes a bind mount from host to container.
type Mount struct {
	Source   string // host path
	Target   string // container path
	ReadOnly bool
}

// EnsureImage builds the container image if it doesn't already exist.
// Safe to call from multiple goroutines — the build is serialized and cached.
func (t *Tool) EnsureImage(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.built {
		return t.buildErr
	}

	t.buildErr = t.buildImage(ctx)
	t.built = true
	return t.buildErr
}

// Run executes the tool in a container, passing input via exec stdin and
// returning stdout. The image is built automatically if needed.
// All operations use the embedded Podman REST API — no external binary.
func (t *Tool) Run(ctx context.Context, input []byte) ([]byte, error) {
	if t.Engine == nil {
		return nil, fmt.Errorf("containertool %s: no container engine configured", t.Name)
	}

	if err := t.EnsureImage(ctx); err != nil {
		return nil, fmt.Errorf("ensure %s image: %w", t.Name, err)
	}

	runTimeout := t.RunTimeout
	if runTimeout == 0 {
		runTimeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	// Build container config with mounts.
	var mounts []container.Mount
	for _, m := range t.Mounts {
		mounts = append(mounts, container.Mount{
			Source:   m.Source,
			Target:   m.Target,
			ReadOnly: m.ReadOnly,
		})
	}

	cfg := &container.ContainerConfig{
		Image:    t.Image,
		ReadOnly: true,
		Mounts:   mounts,
		Command:  []string{"cat"}, // keep container alive for exec
	}

	ctr, err := t.Engine.CreateContainer(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create %s container: %w", t.Name, err)
	}
	defer ctr.Remove(ctx, true)

	if err := ctr.Start(ctx); err != nil {
		return nil, fmt.Errorf("start %s container: %w", t.Name, err)
	}

	// Write input to a temp file and copy into container.
	tmpFile, err := os.CreateTemp("", "containertool-input-*")
	if err != nil {
		return nil, fmt.Errorf("create input temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(input)
	tmpFile.Close()

	if err := ctr.CopyTo(ctx, tmpFile.Name(), "/tmp/input.json"); err != nil {
		return nil, fmt.Errorf("copy input to %s container: %w", t.Name, err)
	}

	// Execute the tool with input from the copied file.
	result, err := ctr.Exec(ctx, "cat /tmp/input.json | /app/tool", "")
	if err != nil {
		return nil, fmt.Errorf("exec %s: %w", t.Name, err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("tool %s exited with code %d: %s", t.Name, result.ExitCode, result.Stderr)
	}

	return []byte(result.Stdout), nil
}

// RunJSON is a convenience wrapper that marshals input and unmarshals output.
func (t *Tool) RunJSON(ctx context.Context, input any, output any) error {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal %s input: %w", t.Name, err)
	}

	out, err := t.Run(ctx, inputJSON)
	if err != nil {
		return err
	}

	if output != nil {
		if err := json.Unmarshal(out, output); err != nil {
			return fmt.Errorf("unmarshal %s output: %w", t.Name, err)
		}
	}

	return nil
}

// Available returns true if the embedded container engine is healthy.
func (t *Tool) Available() bool {
	return t.Engine != nil && t.Engine.Healthy()
}

func (t *Tool) buildImage(ctx context.Context) error {
	if t.Engine == nil {
		return fmt.Errorf("no container engine configured")
	}

	// Check if image already exists via REST API.
	if t.Engine.ImageExists(ctx, t.Image) {
		slog.Debug("container tool image exists", "tool", t.Name, "image", t.Image)
		return nil
	}

	slog.Info("building container tool image (one-time)", "tool", t.Name, "image", t.Image)

	// Write sources to a temp build context.
	buildDir, err := os.MkdirTemp("", "ycode-"+t.Name+"-build-*")
	if err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	// Write Dockerfile.
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(t.Dockerfile), 0644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	// Write source files.
	for name, content := range t.Sources {
		if err := os.WriteFile(filepath.Join(buildDir, name), []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}

	// Build via REST API.
	if err := t.Engine.BuildImage(ctx, t.Image, []byte(t.Dockerfile)); err != nil {
		return fmt.Errorf("build %s image: %w", t.Name, err)
	}

	slog.Info("container tool image built", "tool", t.Name, "image", t.Image)
	return nil
}
