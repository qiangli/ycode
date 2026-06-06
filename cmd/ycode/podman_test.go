package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestPodmanCmdStructure pins the post-pass-through shape: parent
// `podman` is a thin shell that exec's the embedded binary; the only
// registered subcommands are the ycode-specific extensions
// (`machine` lifecycle, `cleanup`) that have no upstream parallel.
// Every other verb (ps / images / push / pull / run / inspect /
// version / build / network / volume / system / …) flows straight
// to upstream podman via DisableFlagParsing + ArbitraryArgs +
// parent RunE.
func TestPodmanCmdStructure(t *testing.T) {
	cmd := newPodmanCmd()

	if cmd.Use == "" || !strings.HasPrefix(cmd.Use, "podman") {
		t.Errorf("expected Use to start with 'podman', got %q", cmd.Use)
	}

	// docker alias preserved — the scripts/shims/docker shim points
	// at `ycode docker` and treats it as an interchangeable surface.
	if !containsString(cmd.Aliases, "docker") {
		t.Errorf("expected 'docker' alias, got %v", cmd.Aliases)
	}

	// DisableFlagParsing is the load-bearing detail: with it OFF,
	// cobra would reject `--format` / `--filter` / any flag the
	// shallow wrapper didn't predeclare, which is the bug this
	// refactor exists to fix. Pin it.
	if !cmd.DisableFlagParsing {
		t.Error("DisableFlagParsing must be true so all flags pass through to upstream podman")
	}

	// Only ycode-specific extension subcommands stay in the tree.
	// Any other registered subcommand would shadow the upstream
	// verb of the same name and silently revert to the shallow-
	// reimplementation problem.
	allowed := map[string]bool{"machine": true, "cleanup": true}
	for _, sub := range cmd.Commands() {
		if !allowed[sub.Name()] {
			t.Errorf("unexpected subcommand %q — only ycode-specific extensions belong here, everything else passes through", sub.Name())
		}
	}
	for name := range allowed {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing ycode-specific subcommand: %s", name)
		}
	}
}

// TestBuildPodmanExec_SocketEnv asserts the pass-through pins the
// embedded binary at ycode's engine socket via CONTAINER_HOST. The
// upstream podman binary's own socket-discovery logic would
// otherwise prefer the system podman.sock and leave ycode's vfkit
// machine untouched — every container created via `ycode podman …`
// would land in a different store than the one `ycode podman ps`
// reads from. CONTAINER_HOST overrides upstream's discovery and
// keeps both halves on the same socket.
func TestBuildPodmanExec_SocketEnv(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "podman.sock")
	bin := "/usr/bin/podman"
	args := []string{"ps", "-a", "--format", "{{.ID}}"}

	cmd := buildPodmanExec(context.Background(), bin, socket, args)

	if cmd.Path != bin {
		t.Errorf("Path: want %q got %q", bin, cmd.Path)
	}
	wantArgs := append([]string{bin}, args...)
	if !equalStrings(cmd.Args, wantArgs) {
		t.Errorf("Args: want %v got %v", wantArgs, cmd.Args)
	}

	want := "CONTAINER_HOST=unix://" + socket
	if !containsString(cmd.Env, want) {
		t.Errorf("Env missing %q\nfull env: %v", want, cmd.Env)
	}
}

// TestBuildPodmanExec_NoSocket — when the engine isn't running and
// DefaultSocketPath returns "", we still construct a valid command
// (just with no CONTAINER_HOST override). The pass-through then
// surfaces upstream's own "Cannot connect" error rather than
// failing earlier with a confusing one.
func TestBuildPodmanExec_NoSocket(t *testing.T) {
	cmd := buildPodmanExec(context.Background(), "/usr/bin/podman", "", []string{"version"})
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "CONTAINER_HOST=") {
			// Only a passthrough from os.Environ() is OK — we
			// must NOT have appended our own empty unix:// value.
			if kv == "CONTAINER_HOST=unix://" {
				t.Errorf("buildPodmanExec must not stamp an empty CONTAINER_HOST: %q", kv)
			}
		}
	}
}

// TestPodmanCacheDir keeps the per-user cache shape pinned — the
// embedded binary self-extracts once into <user-cache>/ycode/bin and
// is sha256-validated on every subsequent run, so a stable path is
// what makes "extract once" actually save time across invocations.
func TestPodmanCacheDir(t *testing.T) {
	dir := podmanCacheDir()
	if dir == "" {
		t.Fatal("podmanCacheDir returned empty")
	}
	if !strings.Contains(dir, "ycode") {
		t.Errorf("cache dir should namespace under ycode, got %q", dir)
	}
}

// helpers — kept in-test (no need to grow internal/ for two
// trivial slice predicates).

func containsString(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
