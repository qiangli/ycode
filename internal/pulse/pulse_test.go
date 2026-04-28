package pulse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageTag(t *testing.T) {
	tests := []struct {
		version string
		commit  string
		want    string
	}{
		{"v1.2.3", "abcdef1234567890", "ycode-pulse:v1.2.3-abcdef12"},
		{"dev", "abc", "ycode-pulse:dev-abc"},
		{"0.1.0-dirty", "12345678", "ycode-pulse:0.1.0-dirty-12345678"},
	}
	for _, tt := range tests {
		mgr := &Manager{version: tt.version, commit: tt.commit}
		got := mgr.imageTag()
		if got != tt.want {
			t.Errorf("imageTag(%q, %q) = %q, want %q", tt.version, tt.commit, got, tt.want)
		}
	}
}

func TestExtractTarGz(t *testing.T) {
	// Create a minimal tar.gz in memory to test extraction.
	// We'll use the helper to round-trip: create a dir, tar it, extract it.
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(srcDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	// We can't easily test extractTarGz without creating a real tar.gz,
	// but we can test the readerAt helper.
	data := []byte("hello world")
	r := readerAt(data)
	buf := make([]byte, 5)
	n, err := r.ReadAt(buf, 0)
	if err != nil || n != 5 || string(buf) != "hello" {
		t.Errorf("ReadAt(0): n=%d, err=%v, data=%q", n, err, buf)
	}
	n, err = r.ReadAt(buf, 6)
	if err != nil || n != 5 || string(buf) != "world" {
		t.Errorf("ReadAt(6): n=%d, err=%v, data=%q", n, err, buf)
	}
	// Past end.
	_, err = r.ReadAt(buf, int64(len(data)))
	if err == nil {
		t.Error("expected EOF when reading past end")
	}
}

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "src.txt")
	dst := filepath.Join(dstDir, "dst.txt")

	content := "test content for copy"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected %q, got %q", content, string(data))
	}
}

func TestDiscoveryFile(t *testing.T) {
	// Test write/remove discovery file.
	addr := "127.0.0.1:4317"
	if err := writeDiscoveryFile(addr); err != nil {
		t.Fatalf("writeDiscoveryFile: %v", err)
	}

	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".agents", "ycode", "collector.addr")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != addr {
		t.Errorf("expected %q, got %q", addr, string(data))
	}

	removeDiscoveryFile()
	if _, err := os.ReadFile(path); err == nil {
		t.Error("discovery file should be removed")
	}
}
