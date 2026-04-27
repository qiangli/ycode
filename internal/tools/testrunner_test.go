package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFramework(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		expected string
	}{
		{
			name:     "go project",
			files:    map[string]string{"go.mod": "module test"},
			expected: "go",
		},
		{
			name:     "python pytest",
			files:    map[string]string{"pyproject.toml": "[tool.pytest]"},
			expected: "pytest",
		},
		{
			name:     "jest config",
			files:    map[string]string{"jest.config.js": "module.exports = {}"},
			expected: "jest",
		},
		{
			name:     "vitest config",
			files:    map[string]string{"vitest.config.ts": "export default {}"},
			expected: "vitest",
		},
		{
			name:     "cargo project",
			files:    map[string]string{"Cargo.toml": "[package]"},
			expected: "cargo",
		},
		{
			name:     "package.json with jest",
			files:    map[string]string{"package.json": `{"devDependencies": {"jest": "^29"}}`},
			expected: "jest",
		},
		{
			name:     "package.json with vitest",
			files:    map[string]string{"package.json": `{"devDependencies": {"vitest": "^1"}}`},
			expected: "vitest",
		},
		{
			name:     "no framework",
			files:    map[string]string{"README.md": "hello"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
					t.Fatal(err)
				}
			}
			got := detectFramework(dir)
			if got != tt.expected {
				t.Errorf("detectFramework() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRunGoTests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Create a minimal Go project with a passing and failing test.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testproject\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(`package testproject

import "testing"

func TestPass(t *testing.T) {
	if 1+1 != 2 {
		t.Error("math is broken")
	}
}

func TestFail(t *testing.T) {
	t.Error("intentional failure")
}
`), 0644); err != nil {
		t.Fatal(err)
	}

	result := runGoTests(t.Context(), dir, "")

	if result.Framework != "go" {
		t.Errorf("Framework = %q, want %q", result.Framework, "go")
	}
	if result.Passed != 1 {
		t.Errorf("Passed = %d, want 1", result.Passed)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
	if result.Success {
		t.Error("Success should be false with a failing test")
	}
	if len(result.Failures) == 0 {
		t.Fatal("expected at least one failure")
	}

	found := false
	for _, f := range result.Failures {
		if f.File != "" && f.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected at least one failure with file:line location")
	}
}
