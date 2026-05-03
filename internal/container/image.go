package container

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	buildahDefine "go.podman.io/buildah/define"
	"go.podman.io/podman/v6/pkg/bindings/images"
	"go.podman.io/podman/v6/pkg/bindings/system"
	entTypes "go.podman.io/podman/v6/pkg/domain/entities/types"
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

// PullImage pulls a container image from a registry via REST API.
func (e *Engine) PullImage(ctx context.Context, name string) error {
	slog.Info("container: pulling image", "image", name)
	_, err := images.Pull(e.connCtx, name, nil)
	if err != nil {
		return fmt.Errorf("pull image %s: %w", name, err)
	}
	return nil
}

// ListImages lists locally available images via REST API.
func (e *Engine) ListImages(ctx context.Context) ([]ImageInfo, error) {
	listed, err := images.List(e.connCtx, nil)
	if err != nil {
		return nil, err
	}

	var infos []ImageInfo
	for _, img := range listed {
		info := ImageInfo{
			ID:   img.ID,
			Size: img.Size,
		}
		if len(img.RepoTags) > 0 {
			info.Names = img.RepoTags
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// ImageExists checks if an image exists locally via REST API.
func (e *Engine) ImageExists(ctx context.Context, name string) bool {
	exists, err := images.Exists(e.connCtx, name, nil)
	if err != nil {
		return false
	}
	return exists
}

// RemoveImage removes a local image via REST API.
func (e *Engine) RemoveImage(ctx context.Context, name string, force bool) error {
	opts := new(images.RemoveOptions).WithForce(force)
	_, errs := images.Remove(e.connCtx, []string{name}, opts)
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// EnsureImage ensures an image exists locally, pulling it if needed.
func (e *Engine) EnsureImage(ctx context.Context, name string) error {
	if e.ImageExists(ctx, name) {
		return nil
	}
	return e.PullImage(ctx, name)
}

// BuildSandboxImage builds the default ycode agent sandbox image.
func (e *Engine) BuildSandboxImage(ctx context.Context, name string) error {
	if e.ImageExists(ctx, name) {
		slog.Info("container: sandbox image already exists", "image", name)
		return nil
	}
	return e.BuildImage(ctx, name, sandboxDockerfile)
}

// BuildImage builds an image from a Dockerfile byte slice via REST API.
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

	slog.Info("container: building image", "image", name)

	buildOpts := entTypes.BuildOptions{
		BuildOptions: buildahDefine.BuildOptions{
			Output: name,
		},
		ContainerFiles: []string{dockerfilePath},
	}

	_, err = images.Build(e.connCtx, []string{dockerfilePath}, buildOpts)
	if err != nil {
		return fmt.Errorf("build image %s: %w", name, err)
	}
	return nil
}

// BuildImageWithContext builds an image from a Dockerfile byte slice using the
// given directory as the build context. This allows COPY instructions to
// reference files placed in contextDir (e.g., a compiled binary).
func (e *Engine) BuildImageWithContext(ctx context.Context, name string, dockerfile []byte, contextDir string) error {
	dockerfilePath := filepath.Join(contextDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, dockerfile, 0o644); err != nil {
		return fmt.Errorf("write Dockerfile: %w", err)
	}

	slog.Info("container: building image with context", "image", name, "context", contextDir)

	buildOpts := entTypes.BuildOptions{
		BuildOptions: buildahDefine.BuildOptions{
			Output:           name,
			ContextDirectory: contextDir,
		},
		ContainerFiles: []string{dockerfilePath},
	}

	_, err := images.Build(e.connCtx, []string{dockerfilePath}, buildOpts)
	if err != nil {
		return fmt.Errorf("build image %s: %w", name, err)
	}
	return nil
}

// Version returns the podman version string via REST API.
func (e *Engine) Version(ctx context.Context) (string, error) {
	info, err := system.Version(e.connCtx, nil)
	if err != nil {
		return "", err
	}
	if info.Client != nil {
		return info.Client.Version, nil
	}
	if info.Server != nil {
		return info.Server.Version, nil
	}
	return "unknown", nil
}
