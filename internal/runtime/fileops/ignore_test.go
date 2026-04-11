package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewIgnoreChecker_NoFile(t *testing.T) {
	dir := t.TempDir()
	ic := NewIgnoreChecker(dir)
	if ic != nil {
		t.Error("expected nil when no ignore file exists")
	}
}

func TestIgnoreChecker_BasicPatterns(t *testing.T) {
	dir := t.TempDir()
	ignoreContent := `# Comment line
*.log
*.tmp

build/
!important.log
`
	if err := os.WriteFile(filepath.Join(dir, ".ycodeignore"), []byte(ignoreContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a directory for the dirOnly test.
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}

	ic := NewIgnoreChecker(dir)
	if ic == nil {
		t.Fatal("expected non-nil IgnoreChecker")
	}

	tests := []struct {
		path    string
		ignored bool
	}{
		{"debug.log", true},
		{"output.tmp", true},
		{"main.go", false},
		{"important.log", false},            // negated
		{filepath.Join(dir, "build"), true}, // directory pattern
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ic.IsIgnored(tt.path)
			if got != tt.ignored {
				t.Errorf("IsIgnored(%q) = %v, want %v", tt.path, got, tt.ignored)
			}
		})
	}
}

func TestIgnoreChecker_FallbackToGitignore(t *testing.T) {
	dir := t.TempDir()
	gitignoreContent := "*.pyc\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignoreContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ic := NewIgnoreChecker(dir)
	if ic == nil {
		t.Fatal("expected non-nil IgnoreChecker from .gitignore fallback")
	}

	if !ic.IsIgnored("test.pyc") {
		t.Error("expected test.pyc to be ignored via .gitignore")
	}
	if ic.IsIgnored("test.py") {
		t.Error("expected test.py to not be ignored")
	}
}

func TestIgnoreChecker_YcodeignoreOverGitignore(t *testing.T) {
	dir := t.TempDir()
	// Both files exist; .ycodeignore should take precedence.
	if err := os.WriteFile(filepath.Join(dir, ".ycodeignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.pyc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ic := NewIgnoreChecker(dir)
	if ic == nil {
		t.Fatal("expected non-nil IgnoreChecker")
	}

	if !ic.IsIgnored("debug.log") {
		t.Error("expected .log files to be ignored via .ycodeignore")
	}
	if ic.IsIgnored("test.pyc") {
		t.Error("expected .pyc files NOT to be ignored when .ycodeignore exists")
	}
}

func TestIgnoreChecker_NilSafe(t *testing.T) {
	var ic *IgnoreChecker
	if ic.IsIgnored("anything") {
		t.Error("nil IgnoreChecker should not ignore anything")
	}
}
