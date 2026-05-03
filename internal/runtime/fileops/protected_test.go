package fileops

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsProtectedPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("protected paths only apply on macOS")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path     string
		expected bool
	}{
		{filepath.Join(home, "Music"), true},
		{filepath.Join(home, "Music", "iTunes"), true},
		{filepath.Join(home, "Pictures"), true},
		{filepath.Join(home, "Movies"), true},
		{filepath.Join(home, "Downloads"), true},
		{filepath.Join(home, "Desktop"), true},
		{filepath.Join(home, "Documents"), true},
		{filepath.Join(home, "projects"), false},
		{filepath.Join(home, ".config"), false},
		{"/tmp/safe", false},
	}

	for _, tc := range tests {
		got := IsProtectedPath(tc.path)
		if got != tc.expected {
			t.Errorf("IsProtectedPath(%q) = %v, want %v", tc.path, got, tc.expected)
		}
	}
}

func TestIsProtectedPath_NonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("testing non-darwin behavior")
	}

	// On non-macOS, everything should return false.
	home, _ := os.UserHomeDir()
	if IsProtectedPath(filepath.Join(home, "Music")) {
		t.Error("expected false on non-darwin platform")
	}
}
