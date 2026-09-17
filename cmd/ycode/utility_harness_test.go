package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func harnessFixture(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "examples", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigIsReadOnlyCompiledHarnessInspection(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	cmd := testRoot(t)
	if cmd.CommandPath() == "" {
		t.Fatal("command path is empty")
	}
	for _, forbidden := range []string{"set", "unset"} {
		if child, _, err := cmd.Find([]string{"config", forbidden}); err == nil && child.Name() == forbidden {
			t.Fatalf("imperative config command %q is still registered", forbidden)
		}
	}
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"config", "--file", harnessFixture(t), "get", "spec.runtime.defaultAgentRef"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "coder\n" {
		t.Fatalf("config get = %q", output.String())
	}
}

func TestModelToolsMemoryAndSkillsReadCompiledHarness(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	fixture := harnessFixture(t)

	model := testRoot(t)
	for _, forbidden := range []string{"use", "p2p"} {
		if child, _, err := model.Find([]string{"model", forbidden}); err == nil && child.Name() == forbidden {
			t.Fatalf("imperative model command %q is still registered", forbidden)
		}
	}
	var modelOutput bytes.Buffer
	model.SetOut(&modelOutput)
	model.SetArgs([]string{"model", "--file", fixture, "current"})
	if err := model.Execute(); err != nil {
		t.Fatal(err)
	}
	if modelOutput.String() != "gpt-5.6\n" {
		t.Fatalf("model current = %q", modelOutput.String())
	}

	tools := testRoot(t)
	var toolsOutput bytes.Buffer
	tools.SetOut(&toolsOutput)
	tools.SetArgs([]string{"tools", "--file", fixture, "--json", "list"})
	if err := tools.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Count(toolsOutput.String(), `"name": "bashy"`) != 1 || strings.Contains(toolsOutput.String(), `"name": "read_file"`) {
		t.Fatalf("tools = %s", toolsOutput.String())
	}

	memory := testRoot(t)
	for _, forbidden := range []string{"forget", "export"} {
		if child, _, err := memory.Find([]string{"memory", forbidden}); err == nil && child.Name() == forbidden {
			t.Fatalf("legacy memory command %q is still registered", forbidden)
		}
	}
	var memoryOutput bytes.Buffer
	memory.SetOut(&memoryOutput)
	memory.SetArgs([]string{"memory", "--file", fixture, "list"})
	if err := memory.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(memoryOutput.String(), "main\t") {
		t.Fatalf("memory list = %q", memoryOutput.String())
	}

	skills := testRoot(t)
	var skillsOutput bytes.Buffer
	skills.SetOut(&skillsOutput)
	skills.SetArgs([]string{"skill", "--file", fixture, "list"})
	if err := skills.Execute(); err != nil {
		t.Fatal(err)
	}
	if skillsOutput.Len() != 0 {
		t.Fatalf("undeclared skills leaked into inventory: %q", skillsOutput.String())
	}
}

func TestShellOneShotUsesCompiledBashyPolicyBoundary(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	workspace := filepath.Dir(harnessFixture(t))
	err := runHarnessShellOneShot(&shellFlags{harnessFile: harnessFixture(t), workDir: workspace, command: "pwd"})
	if err != nil {
		t.Fatal(err)
	}
	platform, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(platform, "ycode", "harness", "authorization.key")); err != nil {
		t.Fatalf("authorization key: %v", err)
	}
}
