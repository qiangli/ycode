package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestChatConfigPutsTheWorkspaceInTheCallersDirectory(t *testing.T) {
	workspace, dir := t.TempDir(), filepath.Join(t.TempDir(), "instance")
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(filepath.Join(profile, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join("..", "..", "agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	configured := strings.Replace(string(source), "      id: gpt-5.6\n", "      id: s387-selected-model\n", 1)
	if configured == string(source) {
		t.Fatal("model profile was not prepared")
	}
	if err := os.WriteFile(filepath.Join(profile, "agent.yaml"), []byte(configured), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(filepath.Join("..", "..", "prompts", "system.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "prompts", "system.md"), prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := chatConfig(filepath.Join(profile, "agent.yaml"), workspace, dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"\n    workspace: " + strconv.Quote(workspace) + "\n",
		"\n    readableRoots: [" + strconv.Quote(workspace) + ", " + strconv.Quote(dir) + "]\n",
		"\n    writableRoots: [" + strconv.Quote(workspace) + "]\n",
		"      id: s387-selected-model\n",
		"          default: \"\"\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("instance config lacks %q", want)
		}
	}
	if strings.Contains(text, "api.openai.com") || strings.Contains(text, "id: gpt-5.6") {
		t.Fatal("instance config retained the ambient OpenAI fallback or default model")
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "system.md")); err != nil {
		t.Fatalf("prompts not copied beside the config: %v", err)
	}
	// Without -instance-dir the config goes under ~/.bashy/genie/chat.
	t.Setenv("HOME", t.TempDir())
	path, err = chatConfig(filepath.Join(profile, "agent.yaml"), workspace, "")
	if err != nil || !strings.Contains(path, filepath.Join(".bashy", "genie", "chat")) {
		t.Fatalf("default instance dir: %q %v", path, err)
	}
}

func TestNativePathForWindowsDrivePaths(t *testing.T) {
	for _, tc := range []struct{ goos, in, want string }{
		{"windows", "/c/Users/x/agent.yaml", "C:/Users/x/agent.yaml"},
		{"windows", "/d", "D:/"},
		{"windows", "/cd/x", "/cd/x"},
		{"windows", "C:/Users/x", "C:/Users/x"},
		{"windows", "rel/x", "rel/x"},
		{"linux", "/c/Users/x", "/c/Users/x"},
	} {
		if got := nativePathFor(tc.goos, tc.in); got != tc.want {
			t.Errorf("nativePathFor(%q, %q) = %q, want %q", tc.goos, tc.in, got, tc.want)
		}
	}
}
