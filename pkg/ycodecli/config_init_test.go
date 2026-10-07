package ycodecli

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/spec"
)

// runDeclared runs argv through the CLI compiled from the file at path,
// with env as the whole environment the CLI may read.
func runDeclared(t *testing.T, path string, env map[string]string, argv ...string) (string, error) {
	t.Helper()
	doc, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := harnesscli.New(doc, dispatchCLI, harnesscli.Options{Version: version, Commit: commit, LookupEnv: func(name string) (string, bool) {
		value, ok := env[name]
		return value, ok
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(argv)
	err = root.Execute()
	return out.String(), err
}

// genieWorkspace copies genie's authored YAML (and its prompts) into a
// fresh workspace beside other agents' instruction files.
func genieWorkspace(t *testing.T) (string, string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join("..", "..", "examples", "genie")
	data, err := os.ReadFile(filepath.Join(source, "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	prompts, _ := filepath.Glob(filepath.Join(source, "prompts", "*"))
	for _, prompt := range prompts {
		data, err := os.ReadFile(prompt)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "prompts", filepath.Base(prompt)), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	others := map[string]string{"AGENTS.md": "agents\n", "CLAUDE.md": "claude\n", "GEMINI.md": "gemini\n"}
	for name, body := range others {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, path, others
}

func TestGenieInitSeedsGENIEmdIntoContextAndPreservesFiles(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	dir, path, others := genieWorkspace(t)
	genie := filepath.Join(dir, "GENIE.md")

	before, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Spec.Sources["repository-instructions"].Resolved != "" {
		t.Fatal("GENIE.md resolved before it exists")
	}
	out, err := runDeclared(t, path, nil, "--file", path, "init")
	if err != nil || !strings.Contains(out, "created: ") || !strings.Contains(out, "coding/repository") {
		t.Fatalf("init = %q, %v", out, err)
	}
	template := before.Spec.Sources["repository-instructions-template"].Resolved
	if got, _ := os.ReadFile(genie); string(got) != template {
		t.Fatalf("GENIE.md = %q, want the authored template", got)
	}
	after, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Spec.Sources["repository-instructions"].Resolved != template || after.ConfigDigest == before.ConfigDigest {
		t.Fatal("the compiled configuration does not carry GENIE.md")
	}

	authored := "# mine\nkeep this\n"
	if err := os.WriteFile(genie, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runDeclared(t, path, nil, "--file", path, "init")
	if err != nil || !strings.Contains(out, "left unchanged") {
		t.Fatalf("second init = %q, %v", out, err)
	}
	if got, _ := os.ReadFile(genie); string(got) != authored {
		t.Fatalf("init overwrote GENIE.md: %q", got)
	}
	for name, body := range others {
		if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != body {
			t.Fatalf("%s changed: %q", name, got)
		}
	}

	// A directory where GENIE.md belongs fails compilation; nothing replaces it.
	if err := os.Remove(genie); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(genie, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := spec.Load(path); err == nil || !strings.Contains(err.Error(), "repository-instructions") {
		t.Fatalf("a directory GENIE.md compiled: %v", err)
	}
}

func TestConfigSourceReportsOrigin(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	_, path, _ := genieWorkspace(t)
	for _, tc := range []struct {
		name string
		env  map[string]string
		argv []string
		want string
	}{
		{"default", nil, []string{"config", "source"}, "origin: default\n"},
		{"env", map[string]string{"YCODE_CONFIG": path}, []string{"config", "source"}, "origin: env YCODE_CONFIG\n"},
		{"flag", nil, []string{"--file", path, "config", "source"}, "origin: flag\n"},
		{"shorthand", nil, []string{"-f", path, "config", "source"}, "origin: flag\n"},
	} {
		out, err := runDeclared(t, path, tc.env, tc.argv...)
		if err != nil || !strings.Contains(out, tc.want) || !strings.Contains(out, "config: "+path+"\n") || !strings.Contains(out, "name: genie\n") {
			t.Fatalf("%s: config source = %q, %v", tc.name, out, err)
		}
	}
}

func TestConfigUseValidatesCustomFileFailClosed(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	dir, path, _ := genieWorkspace(t)
	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("apiVersion: ycode.dev/v1alpha1\nkind: Harness\nspec: {unknownField: true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{bad, filepath.Join(dir, "missing.yaml"), dir} {
		if out, err := runDeclared(t, path, nil, "--file", path, "config", "use", candidate); err == nil {
			t.Fatalf("config use %s succeeded: %q", candidate, out)
		}
	}
	custom := filepath.Join(dir, "custom.yaml")
	data, err := os.ReadFile(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(custom, data, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runDeclared(t, path, nil, "--file", path, "config", "use", custom)
	if err != nil || !strings.Contains(out, "valid: "+custom) || !strings.Contains(out, "name: ycode") {
		t.Fatalf("config use custom = %q, %v", out, err)
	}
	// Validation selects nothing: the effective configuration is unchanged.
	out, err = runDeclared(t, path, nil, "--file", path, "config", "source")
	if err != nil || !strings.Contains(out, "name: genie\n") {
		t.Fatalf("config source after use = %q, %v", out, err)
	}
}

func TestInitSourceMustReachAContext(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-secret")
	_, path, _ := genieWorkspace(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fragment := "        - id: repository\n          sourceRef: repository-instructions\n          role: system\n          cache: {scope: workspace, breakAfter: true}\n"
	if !bytes.Contains(data, []byte(fragment)) {
		t.Fatal("genie's coding context no longer loads GENIE.md")
	}
	unused := strings.Replace(string(data), fragment, "", 1)
	unused = strings.Replace(unused, "    repository-instructions:\n", "    repository-instructions:\n      exported: true\n", 1)
	if _, err := spec.Compile(path, []byte(unused)); err == nil || !strings.Contains(err.Error(), "not loaded by any context") {
		t.Fatalf("init over an unloaded source compiled: %v", err)
	}
	escaped := strings.Replace(string(data), "path: GENIE.md, base: workspace", "path: GENIE.md, base: elsewhere", 1)
	if _, err := spec.Compile(path, []byte(escaped)); err == nil {
		t.Fatal("an unknown file.base compiled")
	}
}
