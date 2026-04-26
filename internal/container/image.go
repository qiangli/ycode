package container

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

//go:embed Dockerfile.sandbox
var sandboxDockerfile []byte

// ImageInfo holds information about a container image.
type ImageInfo struct {
	ID         string   `json:"Id"`
	Repository string   `json:"Repository"`
	Tag        string   `json:"Tag"`
	Size       int64    `json:"Size"`
	Names      []string `json:"Names"`
}

// PullImage pulls a container image from a registry.
func (e *Engine) PullImage(ctx context.Context, name string) error {
	slog.Info("container: pulling image", "image", name)
	_, err := e.Run(ctx, "pull", name)
	if err != nil {
		return fmt.Errorf("pull image %s: %w", name, err)
	}
	return nil
}

// ListImages lists locally available images.
func (e *Engine) ListImages(ctx context.Context) ([]ImageInfo, error) {
	var images []ImageInfo
	if err := e.RunJSON(ctx, &images, "images"); err != nil {
		return nil, err
	}
	return images, nil
}

// ImageExists checks if an image exists locally.
func (e *Engine) ImageExists(ctx context.Context, name string) bool {
	_, err := e.Run(ctx, "image", "exists", name)
	return err == nil
}

// RemoveImage removes a local image.
func (e *Engine) RemoveImage(ctx context.Context, name string, force bool) error {
	args := []string{"rmi"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, name)
	_, err := e.Run(ctx, args...)
	return err
}

// EnsureImage ensures an image exists locally, pulling it if needed.
func (e *Engine) EnsureImage(ctx context.Context, name string) error {
	if e.ImageExists(ctx, name) {
		return nil
	}
	return e.PullImage(ctx, name)
}

// BuildSandboxImage builds the default ycode agent sandbox image.
// The Dockerfile is embedded in the binary.
func (e *Engine) BuildSandboxImage(ctx context.Context, name string) error {
	if e.ImageExists(ctx, name) {
		slog.Info("container: sandbox image already exists", "image", name)
		return nil
	}

	// Write Dockerfile to temp directory.
	tmpDir, err := os.MkdirTemp("", "ycode-sandbox-build-*")
	if err != nil {
		return fmt.Errorf("create temp build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, sandboxDockerfile, 0o644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	slog.Info("container: building sandbox image", "image", name)
	_, err = e.Run(ctx, "build", "-t", name, "-f", dockerfilePath, tmpDir)
	if err != nil {
		return fmt.Errorf("build sandbox image: %w", err)
	}

	return nil
}

// BuildImage builds an image from a Dockerfile string.
func (e *Engine) BuildImage(ctx context.Context, name string, dockerfile []byte) error {
	tmpDir, err := os.MkdirTemp("", "ycode-build-*")
	if err != nil {
		return fmt.Errorf("create temp build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, dockerfile, 0o644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	_, err = e.Run(ctx, "build", "-t", name, "-f", dockerfilePath, tmpDir)
	return err
}

// Version returns the podman version string.
func (e *Engine) Version(ctx context.Context) (string, error) {
	out, err := e.Run(ctx, "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
