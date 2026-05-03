package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewTeamMemoryPaths(t *testing.T) {
	tmp := NewTeamMemoryPaths("/home/user/.config/ycode/memory")
	if tmp.TeamDir != "/home/user/.config/ycode/memory/team" {
		t.Errorf("unexpected team dir: %s", tmp.TeamDir)
	}
}

func TestTeamMemoryPaths_EnsureTeamDir(t *testing.T) {
	dir := t.TempDir()
	tmp := NewTeamMemoryPaths(dir)

	if err := tmp.EnsureTeamDir(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(tmp.TeamDir)
	if err != nil {
		t.Fatal("team dir should exist")
	}
	if !info.IsDir() {
		t.Error("should be a directory")
	}
}

func TestTeamMemoryPaths_ValidateTeamPath_Valid(t *testing.T) {
	dir := t.TempDir()
	tmp := NewTeamMemoryPaths(dir)
	if err := tmp.EnsureTeamDir(); err != nil {
		t.Fatal(err)
	}

	// Create a file in the team dir.
	testFile := filepath.Join(tmp.TeamDir, "test.md")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, err := tmp.ValidateTeamPath(testFile)
	if err != nil {
		t.Fatalf("valid path should not error: %v", err)
	}
	if resolved == "" {
		t.Error("resolved path should not be empty")
	}
}

func TestTeamMemoryPaths_ValidateTeamPath_Traversal(t *testing.T) {
	dir := t.TempDir()
	tmp := NewTeamMemoryPaths(dir)
	if err := tmp.EnsureTeamDir(); err != nil {
		t.Fatal(err)
	}

	// Try to access a path outside the team dir.
	outsidePath := filepath.Join(dir, "private.md")
	if err := os.WriteFile(outsidePath, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := tmp.ValidateTeamPath(outsidePath)
	if err == nil {
		t.Error("path traversal should be rejected")
	}
}

func TestIsTeamSafe_Safe(t *testing.T) {
	safe := []string{
		"The project uses PostgreSQL for persistence",
		"Team standup is at 10am daily",
		"Use conventional commits for this repo",
	}
	for _, content := range safe {
		if !IsTeamSafe(content) {
			t.Errorf("should be safe: %q", content)
		}
	}
}

func TestIsTeamSafe_Unsafe(t *testing.T) {
	unsafe := []string{
		"api_key = sk-ant-abcd1234",
		"password: mysecretpass",
		"private_key = /path/to/key.pem",
	}
	for _, content := range unsafe {
		if IsTeamSafe(content) {
			t.Errorf("should be unsafe: %q", content)
		}
	}
}

func TestIsTeamSafe_MentionWithoutAssignment(t *testing.T) {
	// Mentioning "password" in prose (no assignment) should be safe.
	content := "We should implement password reset flow"
	if !IsTeamSafe(content) {
		t.Error("mention without assignment should be safe")
	}
}

func TestAssessMemoryStaleness_Fresh(t *testing.T) {
	s := AssessMemoryStaleness(0)
	if s.IsStale {
		t.Error("0 days should not be stale")
	}
	if s.CaveatText != "" {
		t.Error("fresh memory should have no caveat")
	}
}

func TestAssessMemoryStaleness_Stale(t *testing.T) {
	s := AssessMemoryStaleness(5)
	if !s.IsStale {
		t.Error("5 days should be stale")
	}
	if s.CaveatText == "" {
		t.Error("stale memory should have caveat text")
	}
	if !contains(s.CaveatText, "5 days ago") {
		t.Errorf("caveat should mention age, got: %s", s.CaveatText)
	}
}
