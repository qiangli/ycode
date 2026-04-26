package gitserver

import (
	"testing"
)

func TestComponentConfig(t *testing.T) {
	cfg := &ComponentConfig{
		Enabled:  true,
		DataDir:  "/tmp/test-gitea",
		AppName:  "Test Git",
		HTTPOnly: true,
		Token:    "test-token",
	}
	if !cfg.Enabled {
		t.Error("expected enabled")
	}
	if cfg.AppName != "Test Git" {
		t.Errorf("unexpected app name: %s", cfg.AppName)
	}
}

func TestNewGitServerComponent(t *testing.T) {
	cfg := &ComponentConfig{Enabled: true}
	comp := NewGitServerComponent(cfg, "/tmp/test-gitea")

	if comp.Name() != "git" {
		t.Errorf("unexpected name: %s", comp.Name())
	}
	if comp.Healthy() {
		t.Error("should not be healthy before start")
	}
	if comp.Port() != 0 {
		t.Error("should have no port before start")
	}
	if comp.BaseURL() != "" {
		t.Error("should have no URL before start")
	}
	if comp.HTTPHandler() != nil {
		t.Error("should have no handler before start")
	}
}

func TestGitServerComponentDefaults(t *testing.T) {
	cfg := &ComponentConfig{}
	comp := NewGitServerComponent(cfg, "/tmp/test")

	// Should set default app name.
	if cfg.AppName != "ycode Git" {
		t.Errorf("expected default app name, got %q", cfg.AppName)
	}
	_ = comp
}

func TestServerConfig(t *testing.T) {
	cfg := &ServerConfig{
		DataDir:  "/data/gitea",
		AppName:  "My Git",
		HTTPOnly: true,
		Token:    "abc123",
	}
	if cfg.DataDir != "/data/gitea" {
		t.Error("unexpected data dir")
	}
	if !cfg.HTTPOnly {
		t.Error("expected HTTP only")
	}
}

func TestWorkspaceMode(t *testing.T) {
	if WorkspaceReadOnly != 0 {
		t.Error("WorkspaceReadOnly should be 0")
	}
	if WorkspaceWorktree != 1 {
		t.Error("WorkspaceWorktree should be 1")
	}
}
