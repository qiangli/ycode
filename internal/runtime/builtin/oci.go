package builtin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/container"
)

func init() {
	RegisterSkillExecutor("oci", executeOCI)
}

// executeOCI builds and/or runs a project inside an OCI container using
// ycode's internal embedded podman engine.
func executeOCI(ctx context.Context, args string) (string, error) {
	subcmd, target := parseOCIArgs(args)

	switch subcmd {
	case "build":
		return ociBuild(ctx, target)
	case "run":
		return ociRun(ctx, target)
	default:
		return "", fmt.Errorf("unknown subcommand: %s (expected build or run)", subcmd)
	}
}

// parseOCIArgs splits args into subcommand and target.
// Defaults: subcommand="build", target=".".
func parseOCIArgs(args string) (subcmd, target string) {
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return "build", "."
	}
	switch parts[0] {
	case "build", "run":
		subcmd = parts[0]
		if len(parts) > 1 {
			target = parts[1]
		} else if subcmd == "build" {
			target = "."
		}
		return subcmd, target
	default:
		// No subcommand — treat as build target.
		return "build", parts[0]
	}
}

// ociBuild builds a project inside a container.
func ociBuild(ctx context.Context, target string) (string, error) {
	projectDir, cleanup, err := resolveTarget(ctx, target)
	if err != nil {
		return "", fmt.Errorf("resolve target: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	dockerfile, err := detectDockerfile(projectDir)
	if err != nil {
		return "", fmt.Errorf("detect Dockerfile: %w", err)
	}

	engine, err := container.NewEngine(ctx, &container.EngineConfig{})
	if err != nil {
		return "", fmt.Errorf("start container engine: %w", err)
	}
	defer engine.Close(ctx)

	imageName := fmt.Sprintf("oci-build-%d", time.Now().UnixMilli())

	if err := engine.BuildImageWithContext(ctx, imageName, dockerfile, projectDir); err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}

	// Create container, run default build, collect output.
	ctrCfg := &container.ContainerConfig{
		Name:    fmt.Sprintf("oci-build-%d", time.Now().UnixMilli()),
		Image:   imageName,
		WorkDir: "/src",
		Command: []string{"sleep", "infinity"},
		Labels: map[string]string{
			"ycode.oci": "true",
		},
	}

	ctr, err := engine.CreateContainer(ctx, ctrCfg)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	defer ctr.Remove(ctx, true) //nolint:errcheck

	if err := ctr.Start(ctx); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}
	defer ctr.Stop(ctx, 10*time.Second) //nolint:errcheck

	buildCmd := detectBuildCommand(projectDir)
	result, err := ctr.Exec(ctx, buildCmd, "/src")
	if err != nil {
		return "", fmt.Errorf("exec build: %w", err)
	}

	return formatBuildResult(target, buildCmd, result), nil
}

// ociRun runs an existing image.
func ociRun(ctx context.Context, image string) (string, error) {
	if image == "" {
		return "", fmt.Errorf("run requires an image name")
	}

	engine, err := container.NewEngine(ctx, &container.EngineConfig{})
	if err != nil {
		return "", fmt.Errorf("start container engine: %w", err)
	}
	defer engine.Close(ctx)

	ctrCfg := &container.ContainerConfig{
		Name:  fmt.Sprintf("oci-run-%d", time.Now().UnixMilli()),
		Image: image,
		Labels: map[string]string{
			"ycode.oci": "true",
		},
	}

	ctr, err := engine.CreateContainer(ctx, ctrCfg)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}
	defer ctr.Remove(ctx, true) //nolint:errcheck

	if err := ctr.Start(ctx); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}
	defer ctr.Stop(ctx, 10*time.Second) //nolint:errcheck

	return fmt.Sprintf("Container started from image %s (id: %s)", image, ctr.ID[:12]), nil
}

// resolveTarget converts a target string to a local directory path.
// For GitHub URLs, it clones the repo and returns a cleanup function.
func resolveTarget(_ context.Context, target string) (dir string, cleanup func(), err error) {
	if isRemoteURL(target) {
		tmpDir, err := os.MkdirTemp("", "oci-clone-*")
		if err != nil {
			return "", nil, fmt.Errorf("create temp dir: %w", err)
		}
		cmd := exec.Command("git", "clone", "--depth=1", target, tmpDir)
		if out, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(tmpDir)
			return "", nil, fmt.Errorf("git clone: %s: %w", string(out), err)
		}
		return tmpDir, func() { os.RemoveAll(tmpDir) }, nil
	}

	absDir, err := filepath.Abs(target)
	if err != nil {
		return "", nil, fmt.Errorf("resolve path: %w", err)
	}
	if _, err := os.Stat(absDir); err != nil {
		return "", nil, fmt.Errorf("target not found: %w", err)
	}
	return absDir, nil, nil
}

// isRemoteURL returns true if s looks like a remote git URL.
func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "http://") ||
		strings.HasPrefix(s, "git@")
}

// detectDockerfile finds or generates a Dockerfile for the project.
func detectDockerfile(projectDir string) ([]byte, error) {
	// Check for existing Dockerfile/Containerfile.
	for _, name := range []string{"Dockerfile", "Containerfile", "docker/Dockerfile"} {
		path := filepath.Join(projectDir, name)
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}

	// Generate from project type detection.
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		return []byte(dockerfileGo), nil
	}
	if _, err := os.Stat(filepath.Join(projectDir, "package.json")); err == nil {
		return []byte(dockerfileNode), nil
	}
	if _, err := os.Stat(filepath.Join(projectDir, "Cargo.toml")); err == nil {
		return []byte(dockerfileRust), nil
	}
	if _, err := os.Stat(filepath.Join(projectDir, "requirements.txt")); err == nil {
		return []byte(dockerfilePython), nil
	}

	return nil, fmt.Errorf("no Dockerfile found and could not detect project type in %s", projectDir)
}

// detectBuildCommand infers the build command from project files.
func detectBuildCommand(projectDir string) string {
	if _, err := os.Stat(filepath.Join(projectDir, "Makefile")); err == nil {
		return "make build"
	}
	if _, err := os.Stat(filepath.Join(projectDir, "go.mod")); err == nil {
		return "go build ./..."
	}
	if _, err := os.Stat(filepath.Join(projectDir, "package.json")); err == nil {
		return "npm run build"
	}
	if _, err := os.Stat(filepath.Join(projectDir, "Cargo.toml")); err == nil {
		return "cargo build"
	}
	return "echo 'no build command detected'"
}

func formatBuildResult(target, buildCmd string, result *container.ExecResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## OCI Build: %s\n\n", target))
	b.WriteString(fmt.Sprintf("**Command:** `%s`\n", buildCmd))
	if result.ExitCode == 0 {
		b.WriteString("**Status:** success\n")
	} else {
		b.WriteString(fmt.Sprintf("**Status:** failed (exit code %d)\n", result.ExitCode))
	}
	if result.Stdout != "" {
		b.WriteString(fmt.Sprintf("\n**Stdout:**\n```\n%s\n```\n", result.Stdout))
	}
	if result.Stderr != "" {
		b.WriteString(fmt.Sprintf("\n**Stderr:**\n```\n%s\n```\n", result.Stderr))
	}
	return b.String()
}

// Language-specific Dockerfile templates.
const dockerfileGo = `FROM docker.io/library/golang:1.26-bookworm
WORKDIR /src
COPY . .
RUN go build ./...
CMD ["sleep", "infinity"]
`

const dockerfileNode = `FROM docker.io/library/node:22-bookworm
WORKDIR /src
COPY package*.json ./
RUN npm ci
COPY . .
CMD ["sleep", "infinity"]
`

const dockerfileRust = `FROM docker.io/library/rust:1-bookworm
WORKDIR /src
COPY . .
RUN cargo build
CMD ["sleep", "infinity"]
`

const dockerfilePython = `FROM docker.io/library/python:3.13-bookworm
WORKDIR /src
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
CMD ["sleep", "infinity"]
`
