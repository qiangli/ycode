package container

import (
	"os"
	"os/exec"
	"testing"
)

func TestDiscoverBinary(t *testing.T) {
	t.Run("explicit path", func(t *testing.T) {
		// Use a known binary as a stand-in.
		path, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash not found")
		}
		got, err := discoverBinary(path)
		if err != nil {
			t.Fatalf("discoverBinary(%q) error: %v", path, err)
		}
		if got != path {
			t.Errorf("got %q, want %q", got, path)
		}
	})

	t.Run("explicit path not found", func(t *testing.T) {
		_, err := discoverBinary("/nonexistent/podman")
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
	})

	t.Run("env var", func(t *testing.T) {
		path, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash not found")
		}
		t.Setenv("YCODE_CONTAINER_RUNTIME", path)
		got, err := discoverBinary("")
		if err != nil {
			t.Fatalf("discoverBinary with env: %v", err)
		}
		if got != path {
			t.Errorf("got %q, want %q", got, path)
		}
	})

	t.Run("system PATH podman", func(t *testing.T) {
		// This test will pass if podman is installed, skip otherwise.
		if _, err := exec.LookPath("podman"); err != nil {
			t.Skip("podman not in PATH")
		}
		got, err := discoverBinary("")
		if err != nil {
			t.Fatalf("discoverBinary from PATH: %v", err)
		}
		if got == "" {
			t.Error("expected non-empty path")
		}
	})
}

func TestDefaultSocketPath(t *testing.T) {
	// Just verify it doesn't panic and returns a string.
	path := defaultSocketPath()
	t.Logf("default socket path: %q", path)
}

func TestSocketAvailable(t *testing.T) {
	// Non-existent socket should return false.
	if socketAvailable("/nonexistent/socket.sock") {
		t.Error("expected false for nonexistent socket")
	}
}

func TestWaitForSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Non-existent socket should timeout quickly.
	err := waitForSocket("/nonexistent/socket.sock", 100*1e6) // 100ms
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestEngineConfig(t *testing.T) {
	cfg := &EngineConfig{
		BinaryPath: "/usr/bin/podman",
		SocketPath: "/tmp/test.sock",
		DataDir:    os.TempDir(),
	}

	if cfg.BinaryPath != "/usr/bin/podman" {
		t.Error("unexpected binary path")
	}
	if cfg.SocketPath != "/tmp/test.sock" {
		t.Error("unexpected socket path")
	}
}
