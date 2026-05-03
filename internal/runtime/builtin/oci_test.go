package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOCIArgs(t *testing.T) {
	tests := []struct {
		args   string
		subcmd string
		target string
	}{
		{"", "build", "."},
		{".", "build", "."},
		{"/path/to/project", "build", "/path/to/project"},
		{"build .", "build", "."},
		{"build /tmp/foo", "build", "/tmp/foo"},
		{"build https://github.com/owner/repo", "build", "https://github.com/owner/repo"},
		{"run myimage:latest", "run", "myimage:latest"},
		{"https://github.com/owner/repo", "build", "https://github.com/owner/repo"},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			subcmd, target := parseOCIArgs(tt.args)
			if subcmd != tt.subcmd {
				t.Errorf("subcmd = %q, want %q", subcmd, tt.subcmd)
			}
			if target != tt.target {
				t.Errorf("target = %q, want %q", target, tt.target)
			}
		})
	}
}

func TestDetectDockerfile(t *testing.T) {
	t.Run("existing Dockerfile", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("FROM alpine\nRUN echo hello\n")
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), content, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("got %q, want %q", got, content)
		}
	})

	t.Run("existing Containerfile", func(t *testing.T) {
		dir := t.TempDir()
		content := []byte("FROM ubuntu\n")
		if err := os.WriteFile(filepath.Join(dir, "Containerfile"), content, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("got %q, want %q", got, content)
		}
	})

	t.Run("Go project", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) == 0 {
			t.Fatal("expected generated Dockerfile")
		}
		if !contains(string(got), "golang") {
			t.Errorf("expected Go Dockerfile, got: %s", got)
		}
	})

	t.Run("Node project", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(string(got), "node") {
			t.Errorf("expected Node Dockerfile, got: %s", got)
		}
	})

	t.Run("Rust project", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname = \"test\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(string(got), "rust") {
			t.Errorf("expected Rust Dockerfile, got: %s", got)
		}
	})

	t.Run("Python project", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !contains(string(got), "python") {
			t.Errorf("expected Python Dockerfile, got: %s", got)
		}
	})

	t.Run("unknown project", func(t *testing.T) {
		dir := t.TempDir()
		_, err := detectDockerfile(dir)
		if err == nil {
			t.Fatal("expected error for unknown project type")
		}
	})

	t.Run("Dockerfile takes priority over go.mod", func(t *testing.T) {
		dir := t.TempDir()
		custom := []byte("FROM custom:latest\n")
		os.WriteFile(filepath.Join(dir, "Dockerfile"), custom, 0o644)
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)

		got, err := detectDockerfile(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(got) != string(custom) {
			t.Errorf("Dockerfile should take priority, got: %s", got)
		}
	})
}

func TestIsRemoteURL(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"https://github.com/owner/repo", true},
		{"http://example.com/repo", true},
		{"git@github.com:owner/repo.git", true},
		{".", false},
		{"/path/to/project", false},
		{"./relative", false},
	}
	for _, tt := range tests {
		if got := isRemoteURL(tt.s); got != tt.want {
			t.Errorf("isRemoteURL(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestDetectBuildCommand(t *testing.T) {
	t.Run("Makefile", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "Makefile"), []byte("build:\n\techo ok\n"), 0o644)
		if got := detectBuildCommand(dir); got != "make build" {
			t.Errorf("got %q, want %q", got, "make build")
		}
	})

	t.Run("go.mod without Makefile", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)
		if got := detectBuildCommand(dir); got != "go build ./..." {
			t.Errorf("got %q, want %q", got, "go build ./...")
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
