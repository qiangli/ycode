package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatConfigHonorsIsolatedBashyHome(t *testing.T) {
	home, state, workspace := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("BASHY_HOME", state)
	config := filepath.Join("..", "..", "agent.yaml")
	path, err := chatConfig(config, workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(state, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("chat config escaped BASHY_HOME: %q (relative %q, %v)", path, relative, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bashy")); !os.IsNotExist(err) {
		t.Fatalf("isolated chat touched the real home: %v", err)
	}
	explicit := t.TempDir()
	path, err = chatConfig(config, workspace, explicit)
	if err != nil || path != filepath.Join(explicit, "agent.yaml") {
		t.Fatalf("explicit instance directory lost precedence: %q, %v", path, err)
	}
}
