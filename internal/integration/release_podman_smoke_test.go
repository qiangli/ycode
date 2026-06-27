//go:build release_smoke

// Release smoke test: exercises the container subsystem end-to-end on
// the bare-minimum use case (pull base + build image + run container)
// so a broken or mis-configured embedded podman path fails CI before
// it lands in a release artifact.
//
// Run with:
//
//	make test-release-smoke
//
// Prereqs:
//   - A reachable podman socket. On Linux, the user socket is usually
//     present; on macOS, `podman machine start` must have been run at
//     least once. The test SKIPS (with a clear message) when no socket
//     is available, so unrelated CI hosts don't fail spuriously.
//
// Smallest image: busybox:musl (~1.5 MB) — used both as the build base
// and the runtime base, so a single pull seeds both legs.

package integration

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	container "github.com/qiangli/coreutils/external/podman/engine"
)

// busyboxImage is the smallest practical OCI image with /bin/echo.
const busyboxImage = "docker.io/library/busybox:musl"

// TestReleaseSmoke_PodmanBuildPullRun confirms the container engine can:
//   - reach a podman socket
//   - pull a base image (busybox:musl, the smallest practical base)
//   - build an image from a Containerfile that derives FROM that base
//   - run the built image and capture its stdout
//
// Total wall-clock: ~5–30 s depending on cache state.
func TestReleaseSmoke_PodmanBuildPullRun(t *testing.T) {
	skipIfNoPodmanSocket(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	engine, err := container.NewEngine(ctx, &container.EngineConfig{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close(ctx)

	// 1. Pull the smallest practical base.
	if err := engine.PullImage(ctx, busyboxImage); err != nil {
		t.Fatalf("PullImage(%s): %v", busyboxImage, err)
	}

	// 2. Build a 2-line image FROM that base. We bake the marker into
	//    the image so the run-side assertion proves we actually executed
	//    *our* image, not the base directly.
	const marker = "ycode-release-smoke-ok"
	containerfile := []byte("FROM " + busyboxImage + "\nCMD [\"echo\", \"" + marker + "\"]\n")
	const builtImage = "ycode-release-smoke:test"
	if err := engine.BuildImage(ctx, builtImage, containerfile); err != nil {
		t.Fatalf("BuildImage: %v", err)
	}

	// 3. Run the built image and verify stdout carries the marker.
	res, err := engine.RunOneShot(ctx, &container.ContainerConfig{
		Image:   builtImage,
		Network: "none",
	})
	if err != nil {
		t.Fatalf("RunOneShot: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", res.ExitCode, res.Stderr)
	}
	if !strings.Contains(res.Stdout, marker) {
		t.Errorf("stdout = %q, want to contain %q", res.Stdout, marker)
	}
}

// skipIfNoPodmanSocket skips when no podman socket is reachable in the
// usual locations. Matches the policy in internal/container/integration_test.go.
func skipIfNoPodmanSocket(t *testing.T) {
	t.Helper()
	for _, p := range candidateSockets() {
		if socketAccepts(p) {
			t.Logf("using podman socket: %s", p)
			return
		}
	}
	t.Skip("no podman socket reachable (start `podman machine` on macOS, or enable user socket on Linux)")
}

// candidateSockets enumerates the standard locations podman uses.
func candidateSockets() []string {
	var paths []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		paths = append(paths, xdg+"/podman/podman.sock")
	}
	if home := os.Getenv("HOME"); home != "" {
		paths = append(paths,
			home+"/.local/share/containers/podman/machine/podman.sock",
			home+"/.local/share/containers/podman/machine/ycode-default/podman.sock",
		)
	}
	paths = append(paths, "/run/podman/podman.sock")
	return paths
}

func socketAccepts(path string) bool {
	if path == "" || strings.HasPrefix(path, "/podman/") {
		return false
	}
	c, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
