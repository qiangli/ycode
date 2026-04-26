package main

import (
	"bytes"
	"testing"
)

func TestPodmanCmdStructure(t *testing.T) {
	cmd := newPodmanCmd()

	if cmd.Use != "podman" {
		t.Errorf("expected Use 'podman', got %q", cmd.Use)
	}

	// Verify the docker alias.
	found := false
	for _, a := range cmd.Aliases {
		if a == "docker" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'docker' alias")
	}

	// Verify subcommands are registered.
	expected := map[string]bool{
		"ps":      false,
		"images":  false,
		"pull":    false,
		"exec":    false,
		"logs":    false,
		"stop":    false,
		"rm":      false,
		"run":     false,
		"version": false,
		"inspect": false,
	}

	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}

func TestPodmanPsFlags(t *testing.T) {
	cmd := newPodmanCmd()
	ps, _, err := cmd.Find([]string{"ps"})
	if err != nil {
		t.Fatalf("find ps: %v", err)
	}
	f := ps.Flags().Lookup("all")
	if f == nil {
		t.Error("ps missing --all flag")
	}
	if f.Shorthand != "a" {
		t.Errorf("expected -a shorthand, got %q", f.Shorthand)
	}
}

func TestPodmanLogsFlags(t *testing.T) {
	cmd := newPodmanCmd()
	logs, _, err := cmd.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("find logs: %v", err)
	}

	f := logs.Flags().Lookup("follow")
	if f == nil {
		t.Error("logs missing --follow flag")
	}
	if f.Shorthand != "f" {
		t.Errorf("expected -f shorthand, got %q", f.Shorthand)
	}

	tail := logs.Flags().Lookup("tail")
	if tail == nil {
		t.Error("logs missing --tail flag")
	}
}

func TestPodmanRmFlags(t *testing.T) {
	cmd := newPodmanCmd()
	rm, _, err := cmd.Find([]string{"rm"})
	if err != nil {
		t.Fatalf("find rm: %v", err)
	}
	f := rm.Flags().Lookup("force")
	if f == nil {
		t.Error("rm missing --force flag")
	}
	if f.Shorthand != "f" {
		t.Errorf("expected -f shorthand, got %q", f.Shorthand)
	}
}

func TestPodmanRunFlags(t *testing.T) {
	cmd := newPodmanCmd()
	run, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("find run: %v", err)
	}

	if rm := run.Flags().Lookup("rm"); rm == nil {
		t.Error("run missing --rm flag")
	}
	if d := run.Flags().Lookup("detach"); d == nil {
		t.Error("run missing --detach flag")
	}
}

func TestPodmanPullArgsValidation(t *testing.T) {
	cmd := newPodmanCmd()

	// pull requires exactly 1 arg — invoke via parent with no image arg.
	cmd.SetArgs([]string{"pull"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Error("pull without args should fail")
	}
}

func TestHelpers(t *testing.T) {
	if got := truncStr("abcdefghijklmnop", 12); got != "abcdefghijkl" {
		t.Errorf("truncStr: got %q", got)
	}
	if got := truncStr("short", 12); got != "short" {
		t.Errorf("truncStr short: got %q", got)
	}

	if got := formatSize(float64(1.5e9)); got != "1.5 GB" {
		t.Errorf("formatSize GB: got %q", got)
	}
	if got := formatSize(float64(50e6)); got != "50.0 MB" {
		t.Errorf("formatSize MB: got %q", got)
	}
	if got := formatSize(float64(1024)); got != "1024 B" {
		t.Errorf("formatSize B: got %q", got)
	}
	if got := formatSize("unknown"); got != "unknown" {
		t.Errorf("formatSize string: got %q", got)
	}
}
