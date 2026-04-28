package container

import (
	"testing"
)

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
		SocketPath: "/tmp/test.sock",
		DataDir:    t.TempDir(),
	}
	if cfg.SocketPath != "/tmp/test.sock" {
		t.Error("unexpected socket path")
	}
}

func TestDefaultBinCacheDir(t *testing.T) {
	dir := defaultBinCacheDir()
	if dir == "" {
		t.Error("expected non-empty cache dir")
	}
}
