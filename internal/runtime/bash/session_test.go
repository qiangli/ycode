package bash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestShellSession_WorkDir(t *testing.T) {
	s := NewShellSession("/tmp")
	if got := s.WorkDir(); got != "/tmp" {
		t.Errorf("expected /tmp, got %q", got)
	}
}

func TestShellSession_ParseOutput_ExtractsCwd(t *testing.T) {
	s := NewShellSession("/initial")

	stdout := "hello world\n\n__YCODE_CWD__\n/new/dir\n"
	cleaned := s.ParseOutput(stdout)

	if got := s.WorkDir(); got != "/new/dir" {
		t.Errorf("expected workDir '/new/dir', got %q", got)
	}
	if cleaned != "hello world" {
		t.Errorf("expected cleaned output 'hello world', got %q", cleaned)
	}
}

func TestShellSession_ParseOutput_NoMarker(t *testing.T) {
	s := NewShellSession("/initial")

	stdout := "just some output\n"
	cleaned := s.ParseOutput(stdout)

	if got := s.WorkDir(); got != "/initial" {
		t.Errorf("expected workDir unchanged, got %q", got)
	}
	if cleaned != "just some output\n" {
		t.Errorf("expected output unchanged, got %q", cleaned)
	}
}

func TestShellSession_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	s := NewShellSession(tmpDir)

	// First command: cd into subdir.
	cmd1 := s.WrapCommand("cd sub && echo in_sub")
	result1, err := Execute(context.Background(), ExecParams{Command: cmd1, WorkDir: tmpDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	cleaned1 := s.ParseOutput(result1.Stdout)
	if cleaned1 != "in_sub" {
		t.Errorf("expected 'in_sub', got %q", cleaned1)
	}
	if got := s.WorkDir(); got != subDir {
		t.Errorf("expected workDir %q, got %q", subDir, got)
	}

	// Second command: should run in the tracked workDir.
	cmd2 := s.WrapCommand("pwd")
	result2, err := Execute(context.Background(), ExecParams{Command: cmd2, WorkDir: tmpDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	cleaned2 := s.ParseOutput(result2.Stdout)
	if cleaned2 != subDir {
		t.Errorf("expected pwd=%q, got %q", subDir, cleaned2)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
