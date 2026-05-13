package origin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/config"
)

func TestProjectNameFromRemote(t *testing.T) {
	cases := map[string]string{
		"https://github.com/foo/bar.git":             "bar",
		"git@github.com:foo/bar":                     "bar",
		"https://example.com/deeply/nested/repo.git": "repo",
		"": "",
	}
	for in, want := range cases {
		if got := projectNameFromRemote(in); got != want {
			t.Fatalf("projectNameFromRemote(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestResolve_NoGitNoOverrides(t *testing.T) {
	// A fresh temp dir is not a git repo. Resolve should fall back to
	// cwd-hash + basename.
	dir := t.TempDir()
	t.Setenv("YCODE_AGENT_TOOL", "")
	t.Setenv("YCODE_PROJECT_NAME", "")
	SetAgentTool(ToolCLIOther)

	o := Resolve(context.Background(), dir, nil)
	if !strings.HasPrefix(o.ProjectID, "cwd-hash:") {
		t.Fatalf("ProjectID = %q; want cwd-hash:*", o.ProjectID)
	}
	if o.ProjectName != filepath.Base(dir) {
		t.Fatalf("ProjectName = %q; want %q", o.ProjectName, filepath.Base(dir))
	}
	if o.ProjectRoot == "" || !filepath.IsAbs(o.ProjectRoot) {
		t.Fatalf("ProjectRoot should be abs; got %q", o.ProjectRoot)
	}
	if o.AgentTool != ToolCLIOther {
		t.Fatalf("AgentTool = %q; want %q", o.AgentTool, ToolCLIOther)
	}
}

func TestResolve_EnvAgentToolOverride(t *testing.T) {
	t.Setenv("YCODE_AGENT_TOOL", "custom-test-tool")
	SetAgentTool(ToolTUI) // env should win

	o := Resolve(context.Background(), t.TempDir(), nil)
	if o.AgentTool != "custom-test-tool" {
		t.Fatalf("AgentTool = %q; want env override", o.AgentTool)
	}
}

func TestResolve_EnvProjectNameOverride(t *testing.T) {
	t.Setenv("YCODE_PROJECT_NAME", "my-project")

	o := Resolve(context.Background(), t.TempDir(), nil)
	if o.ProjectName != "my-project" {
		t.Fatalf("ProjectName = %q; want env override", o.ProjectName)
	}
}

func TestResolve_ConfigOverridesEnv(t *testing.T) {
	t.Setenv("YCODE_PROJECT_NAME", "from-env")
	cfg := &config.Config{Project: &config.ProjectConfig{
		ID:   "explicit-id",
		Name: "from-config",
	}}
	o := Resolve(context.Background(), t.TempDir(), cfg)
	if o.ProjectID != "explicit-id" {
		t.Fatalf("ProjectID = %q; want explicit-id", o.ProjectID)
	}
	if o.ProjectName != "from-config" {
		t.Fatalf("ProjectName = %q; want from-config (cfg beats env)", o.ProjectName)
	}
}

func TestResolve_PersonalityPassthrough(t *testing.T) {
	cfg := &config.Config{Personality: "pirate"}
	o := Resolve(context.Background(), t.TempDir(), cfg)
	if o.Personality != "pirate" {
		t.Fatalf("Personality = %q; want pirate", o.Personality)
	}
}

func TestSetAgentTool_RaceSafety(t *testing.T) {
	// Sanity: the package-level state is mutex-guarded; concurrent
	// callers don't trip the race detector.
	done := make(chan struct{}, 2)
	go func() {
		for i := 0; i < 1000; i++ {
			SetAgentTool(ToolTUI)
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			_ = CurrentAgentTool()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// Ensure os.Getenv lookups don't get false-matched by inherited env
// vars in the test binary's parent shell.
func TestResolve_EmptyEnvFallsThrough(t *testing.T) {
	t.Setenv("YCODE_AGENT_TOOL", "")
	t.Setenv("YCODE_PROJECT_NAME", "")
	SetAgentTool(ToolPrompt)

	o := Resolve(context.Background(), os.TempDir(), nil)
	if o.AgentTool != ToolPrompt {
		t.Fatalf("AgentTool = %q; empty env should let SetAgentTool win", o.AgentTool)
	}
}
