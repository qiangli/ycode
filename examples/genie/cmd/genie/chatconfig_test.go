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
	path, err := chatConfig(filepath.Join("..", "..", "agent.yaml"), workspace, dir)
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
		"\n    writableRoots: [" + strconv.Quote(workspace) + "]\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("instance config lacks %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "system.md")); err != nil {
		t.Fatalf("prompts not copied beside the config: %v", err)
	}
	// Without -instance-dir the config goes under ~/.bashy/genie/chat.
	t.Setenv("HOME", t.TempDir())
	path, err = chatConfig(filepath.Join("..", "..", "agent.yaml"), workspace, "")
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
