// Package containertool provides a standardized pattern for running tools
// inside containers. This is the recommended approach for tools that:
//
//   - Require languages/runtimes other than Go (Python, Rust, C, etc.)
//   - Are written in Go but have heavy CGO dependencies (tree-sitter, etc.)
//   - Are too large or too infrequently used to embed in the core ycode binary
//
// The pattern: define a tool's source code and Dockerfile as string constants,
// build the image once (cached by the container runtime), and invoke it via
// stdin/stdout JSON protocol.
//
// Usage:
//
//	tool := &containertool.Tool{
//	    Name:       "my-tool",
//	    Image:      "ycode-my-tool:latest",
//	    Dockerfile: myDockerfile,
//	    Sources:    map[string]string{"main.go": mainSource, "go.mod": goMod},
//	    Mounts:     []containertool.Mount{{Source: workDir, Target: "/workspace", ReadOnly: true}},
//	}
//	output, err := tool.Run(ctx, inputJSON)
package containertool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Tool defines a containerized tool that can be built and invoked.
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

// Run executes the tool in a container, passing input via stdin and
// returning stdout. The image is built automatically if needed.
func (t *Tool) Run(ctx context.Context, input []byte) ([]byte, error) {
	if err := t.EnsureImage(ctx); err != nil {
		return nil, fmt.Errorf("ensure %s image: %w", t.Name, err)
	}

	runtime := FindContainerRuntime()
	if runtime == "" {
		return nil, fmt.Errorf("no container runtime (podman/docker) found")
	}

	runTimeout := t.RunTimeout
	if runTimeout == 0 {
		runTimeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	args := []string{"run", "--rm", "-i", "--read-only"}

	for _, m := range t.Mounts {
		mode := "rw"
		if m.ReadOnly {
			mode = "ro"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.Source, m.Target, mode))
	}

	args = append(args, t.Image)

	cmd := exec.CommandContext(ctx, runtime, args...)
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("run %s container: %w (%s)", t.Name, err, stderr.String())
	}

	return stdout.Bytes(), nil
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

func (t *Tool) buildImage(ctx context.Context) error {
	runtime := FindContainerRuntime()
	if runtime == "" {
		return fmt.Errorf("no container runtime (podman/docker) found")
	}

	// Check if image already exists.
	checkCmd := exec.CommandContext(ctx, runtime, "image", "inspect", t.Image)
	if err := checkCmd.Run(); err == nil {
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

	buildTimeout := t.BuildTimeout
	if buildTimeout == 0 {
		buildTimeout = 5 * time.Minute
	}
	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(buildCtx, runtime, "build",
		"-t", t.Image,
		"-f", filepath.Join(buildDir, "Dockerfile"),
		buildDir,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s image: %w", t.Name, err)
	}

	slog.Info("container tool image built", "tool", t.Name, "image", t.Image)
	return nil
}

// FindContainerRuntime returns the path to podman or docker, or empty string.
func FindContainerRuntime() string {
	if path, err := exec.LookPath("podman"); err == nil {
		return path
	}
	if path, err := exec.LookPath("docker"); err == nil {
		return path
	}
	return ""
}

// Available returns true if a container runtime is available on the system.
func Available() bool {
	return FindContainerRuntime() != ""
}
