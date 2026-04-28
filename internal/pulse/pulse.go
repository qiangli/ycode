// Package pulse manages the containerized ycode observability stack.
// "Pulse" runs the full OTEL stack (Jaeger, Prometheus, VictoriaLogs,
// collector, dashboards) inside a podman container, keeping it alive
// across ycode CLI invocations until explicitly stopped.
package pulse

import (
	"archive/tar"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/pulse/source_embed"
)

const (
	// ContainerName is the well-known name for the pulse container.
	ContainerName = "ycode-pulse"

	// ImageName is the base name for the pulse container image.
	ImageName = "ycode-pulse"

	// BuilderImageName is the base name for the builder image.
	BuilderImageName = "ycode-pulse-builder"

	// GolangImage is the Go toolchain image used to build ycode from source.
	GolangImage = "docker.io/library/golang:1.24-alpine"

	// DefaultProxyPort is the default HTTP proxy port exposed by pulse.
	DefaultProxyPort = 58080

	// DefaultCollectorPort is the default gRPC OTEL collector port.
	DefaultCollectorPort = 4317

	// DefaultNATSPort is the default NATS message bus port.
	DefaultNATSPort = 4222

	// pollTimeout is how long to wait for the collector to become reachable.
	pollTimeout = 30 * time.Second

	// pollInterval is the delay between TCP dial attempts.
	pollInterval = 500 * time.Millisecond
)

//go:embed Dockerfile.pulse
var dockerfilePulse []byte

// Manager manages the pulse container lifecycle.
type Manager struct {
	engine  *container.Engine
	version string
	commit  string
	dataDir string // host directory for persistent data (~/.agents/ycode/observability)
}

// NewManager creates a pulse manager.
func NewManager(engine *container.Engine, version, commit string) *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{
		engine:  engine,
		version: version,
		commit:  commit,
		dataDir: filepath.Join(home, ".agents", "ycode", "observability"),
	}
}

// imageTag returns the versioned image tag for the current binary.
func (m *Manager) imageTag() string {
	c := m.commit
	if len(c) > 8 {
		c = c[:8]
	}
	return fmt.Sprintf("%s:%s-%s", ImageName, m.version, c)
}

// Start ensures the pulse container is running. If already running, returns nil.
// If the image doesn't exist, it builds it from the embedded source archive.
func (m *Manager) Start(ctx context.Context) error {
	// Check if already running.
	c := container.NewContainer(m.engine, ContainerName)
	if c.IsRunning(ctx) {
		slog.Info("pulse: already running")
		return nil
	}

	// Remove stale stopped container.
	_ = c.Remove(ctx, true)

	// Ensure the image exists.
	if err := m.ensureImage(ctx); err != nil {
		return fmt.Errorf("pulse: ensure image: %w", err)
	}

	// Ensure data directory exists.
	if err := os.MkdirAll(m.dataDir, 0o755); err != nil {
		return fmt.Errorf("pulse: create data dir: %w", err)
	}

	// Create and start the container.
	cfg := &container.ContainerConfig{
		Name:  ContainerName,
		Image: m.imageTag(),
		Command: []string{
			"serve",
			"--port", fmt.Sprintf("%d", DefaultProxyPort),
		},
		Ports: []container.PortMapping{
			{HostPort: DefaultCollectorPort, ContainerPort: DefaultCollectorPort},
			{HostPort: DefaultProxyPort, ContainerPort: DefaultProxyPort},
			{HostPort: DefaultNATSPort, ContainerPort: DefaultNATSPort},
		},
		Mounts: []container.Mount{
			{Source: m.dataDir, Target: "/data", ReadOnly: false},
		},
		Labels: map[string]string{
			"ycode.managed":   "true",
			"ycode.component": "pulse",
		},
		Init:    true,
		CapDrop: []string{}, // don't drop caps — pulse needs to bind ports
	}

	ctr, err := m.engine.CreateContainer(ctx, cfg)
	if err != nil {
		return fmt.Errorf("pulse: create container: %w", err)
	}

	if err := ctr.Start(ctx); err != nil {
		return fmt.Errorf("pulse: start container: %w", err)
	}

	slog.Info("pulse: container started", "name", ContainerName, "image", m.imageTag())

	// Wait for collector to become reachable.
	addr := fmt.Sprintf("127.0.0.1:%d", DefaultCollectorPort)
	if err := waitForPort(ctx, addr, pollTimeout); err != nil {
		slog.Warn("pulse: collector not reachable after start", "addr", addr, "error", err)
	}

	// Write discovery file so CLI instances auto-connect.
	if err := writeDiscoveryFile(addr); err != nil {
		slog.Warn("pulse: failed to write discovery file", "error", err)
	}

	return nil
}

// Stop stops and removes the pulse container.
func (m *Manager) Stop(ctx context.Context) error {
	c := container.NewContainer(m.engine, ContainerName)
	if err := c.Stop(ctx, 10*time.Second); err != nil {
		slog.Debug("pulse: stop returned error (may be already stopped)", "error", err)
	}
	if err := c.Remove(ctx, true); err != nil {
		return fmt.Errorf("pulse: remove container: %w", err)
	}
	removeDiscoveryFile()
	slog.Info("pulse: stopped and removed")
	return nil
}

// Status returns a human-readable status string.
func (m *Manager) Status(ctx context.Context) string {
	c := container.NewContainer(m.engine, ContainerName)
	if c.IsRunning(ctx) {
		return fmt.Sprintf("running (image: %s, ports: %d/%d/%d)",
			m.imageTag(), DefaultCollectorPort, DefaultProxyPort, DefaultNATSPort)
	}
	// Check if container exists but is stopped.
	infos, err := m.engine.ListContainers(ctx, map[string]string{"name": ContainerName})
	if err == nil && len(infos) > 0 {
		return fmt.Sprintf("stopped (image: %s)", m.imageTag())
	}
	return "not running"
}

// ensureImage checks if the pulse image exists, building it if needed.
func (m *Manager) ensureImage(ctx context.Context) error {
	tag := m.imageTag()
	if m.engine.ImageExists(ctx, tag) {
		slog.Info("pulse: image exists", "image", tag)
		return nil
	}

	if !source_embed.Available() {
		return fmt.Errorf("source archive not embedded (build with -tags embed_source)")
	}

	slog.Info("pulse: building image from embedded source", "image", tag)

	// Extract source archive to temp dir.
	srcDir, err := os.MkdirTemp("", "ycode-pulse-src-*")
	if err != nil {
		return fmt.Errorf("create temp source dir: %w", err)
	}
	defer os.RemoveAll(srcDir)

	if err := extractTarGz(source_embed.SourceArchive, srcDir); err != nil {
		return fmt.Errorf("extract source archive: %w", err)
	}

	// Build the linux binary using a golang:alpine container.
	linuxBinary, err := m.buildLinuxBinary(ctx, srcDir)
	if err != nil {
		return fmt.Errorf("build linux binary: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(linuxBinary))

	// Build the pulse image with the linux binary.
	buildDir, err := os.MkdirTemp("", "ycode-pulse-build-*")
	if err != nil {
		return fmt.Errorf("create temp build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	// Copy binary into build context as "ycode".
	dst := filepath.Join(buildDir, "ycode")
	if err := copyFile(linuxBinary, dst); err != nil {
		return fmt.Errorf("copy binary to build context: %w", err)
	}
	// Ensure executable permission.
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("chmod binary: %w", err)
	}

	if err := m.engine.BuildImageWithContext(ctx, tag, dockerfilePulse, buildDir); err != nil {
		return fmt.Errorf("build pulse image: %w", err)
	}

	slog.Info("pulse: image built", "image", tag)
	return nil
}

// buildLinuxBinary compiles ycode for linux using a golang:alpine container.
// Returns the path to the compiled binary.
func (m *Manager) buildLinuxBinary(ctx context.Context, srcDir string) (string, error) {
	slog.Info("pulse: compiling linux binary in container")

	// Ensure golang image is available.
	if err := m.engine.EnsureImage(ctx, GolangImage); err != nil {
		return "", fmt.Errorf("pull golang image: %w", err)
	}

	// Create output directory on host.
	outDir, err := os.MkdirTemp("", "ycode-pulse-out-*")
	if err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	goarch := runtime.GOARCH

	cfg := &container.ContainerConfig{
		Name:  "ycode-pulse-builder",
		Image: GolangImage,
		Command: []string{
			"sh", "-c",
			fmt.Sprintf("cd /src && GOOS=linux GOARCH=%s CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -X main.version=%s -X main.commit=%s' -o /out/ycode ./cmd/ycode/",
				goarch, m.version, m.commit),
		},
		Mounts: []container.Mount{
			{Source: srcDir, Target: "/src", ReadOnly: true},
			{Source: outDir, Target: "/out", ReadOnly: false},
		},
		Labels: map[string]string{
			"ycode.managed":   "true",
			"ycode.component": "pulse-builder",
		},
	}

	// Remove any stale builder container.
	old := container.NewContainer(m.engine, "ycode-pulse-builder")
	_ = old.Remove(ctx, true)

	ctr, err := m.engine.CreateContainer(ctx, cfg)
	if err != nil {
		os.RemoveAll(outDir)
		return "", fmt.Errorf("create builder container: %w", err)
	}

	if err := ctr.Start(ctx); err != nil {
		_ = ctr.Remove(ctx, true)
		os.RemoveAll(outDir)
		return "", fmt.Errorf("start builder container: %w", err)
	}

	// Wait for builder to finish (poll IsRunning).
	deadline := time.After(5 * time.Minute)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			_ = ctr.Stop(ctx, 5*time.Second)
			_ = ctr.Remove(ctx, true)
			os.RemoveAll(outDir)
			return "", fmt.Errorf("builder timed out after 5 minutes")
		case <-tick.C:
			if !ctr.IsRunning(ctx) {
				goto done
			}
		case <-ctx.Done():
			_ = ctr.Stop(ctx, 5*time.Second)
			_ = ctr.Remove(ctx, true)
			os.RemoveAll(outDir)
			return "", ctx.Err()
		}
	}
done:
	_ = ctr.Remove(ctx, true)

	binaryPath := filepath.Join(outDir, "ycode")
	if _, err := os.Stat(binaryPath); err != nil {
		// Try to get build logs for diagnostics.
		logs, _ := m.engine.ContainerLogs(ctx, ctr.ID, false, "50")
		os.RemoveAll(outDir)
		return "", fmt.Errorf("build failed — binary not found at %s\n%s", binaryPath, logs)
	}

	slog.Info("pulse: linux binary built", "path", binaryPath)
	return binaryPath, nil
}

// --- helpers ---

// waitForPort polls a TCP address until it's reachable or timeout expires.
func waitForPort(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return fmt.Errorf("timeout waiting for %s", addr)
		case <-ctx.Done():
			return ctx.Err()
		default:
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				return nil
			}
			time.Sleep(pollInterval)
		}
	}
}

// writeDiscoveryFile writes the collector address so CLI instances auto-connect.
func writeDiscoveryFile(addr string) error {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".agents", "ycode", "collector.addr")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(addr), 0o644)
}

// removeDiscoveryFile removes the collector discovery file.
func removeDiscoveryFile() {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".agents", "ycode", "collector.addr")
	os.Remove(path)
}

// extractTarGz extracts a gzipped tar archive into the given directory.
func extractTarGz(data []byte, dir string) error {
	gr, err := gzip.NewReader(io.NopCloser(io.NewSectionReader(
		readerAt(data), 0, int64(len(data)))))
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}

		target := filepath.Join(dir, hdr.Name)
		// Prevent path traversal.
		rel, err := filepath.Rel(dir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
	return nil
}

// readerAt wraps a byte slice as an io.ReaderAt.
type readerAt []byte

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(r)) {
		return 0, io.EOF
	}
	n := copy(p, r[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
