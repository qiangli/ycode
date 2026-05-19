package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// PodmanSocketPath picks a per-process socket path for the gateway's
// podman endpoint. Per-PID so two `ycode serve` invocations on the same
// box don't collide; per-runtime-dir so the socket lives under a path
// short enough for AF_UNIX (Linux's 108-char limit, macOS' 104).
//
//   - Linux: $XDG_RUNTIME_DIR/ycode-<pid>/podman.sock when set, otherwise
//     /tmp/ycode-<pid>/podman.sock.
//   - macOS / others: ~/.agents/ycode/podman-<pid>.sock, or /tmp/... if
//     the home dir is unusable.
//   - Windows: \\.\pipe\ycode-podman-<pid> (named pipe, not AF_UNIX —
//     ycode-serve on Windows uses the named-pipe transport).
//
// The returned directory is created with 0o700 so other local users
// can't connect to a daemon they don't own.
func PodmanSocketPath() (string, error) {
	pid := os.Getpid()
	switch runtime.GOOS {
	case "windows":
		return fmt.Sprintf(`\\.\pipe\ycode-podman-%d`, pid), nil
	case "linux":
		if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
			dir := filepath.Join(rt, fmt.Sprintf("ycode-%d", pid))
			if err := os.MkdirAll(dir, 0o700); err == nil {
				return filepath.Join(dir, "podman.sock"), nil
			}
		}
	}
	// macOS or Linux without a usable XDG_RUNTIME_DIR — try home, fall
	// back to /tmp.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dir := filepath.Join(home, ".agents", "ycode")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return filepath.Join(dir, fmt.Sprintf("podman-%d.sock", pid)), nil
		}
	}
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("ycode-%d", pid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create runtime dir %s: %w", dir, err)
	}
	return filepath.Join(dir, "podman.sock"), nil
}
