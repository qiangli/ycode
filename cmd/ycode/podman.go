package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/coreutils/external/podman/engine/podman_embed"
)

// newPodmanCmd is a thin pass-through to the embedded upstream
// podman binary built from the external/podman submodule. ycode does
// NOT reimplement podman's verb surface — every verb, flag, and
// output format upstream supports works here automatically. The
// only sub-commands ycode owns are extensions that don't exist in
// upstream:
//
//   - machine: ycode's vfkit-based VM lifecycle (upstream `podman
//     machine` uses qemu and doesn't know about ycode's embedded
//     vfkit/gvproxy, so this stays a ycode-managed surface).
//   - cleanup: prune orphaned vfkit/gvproxy processes + stale
//     sockets (ycode-host-state housekeeping, no podman parallel).
//
// Everything else dispatches to the upstream binary with
// CONTAINER_HOST pointed at ycode's running engine socket, so
// containers + images created via `ycode podman …` land in the same
// store the rest of ycode (sandbox, agent_shell, ollama-via-podman)
// uses.
//
// --use-system-binaries (or container.useSystem in settings.json)
// makes the pass-through defer to whatever podman is on $PATH
// instead of the embedded one. If neither the embed nor a PATH
// podman is present, the command surfaces a clear "no podman
// available" error.
func newPodmanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "podman [ARGS...]",
		Aliases: []string{"docker"},
		Short:   "Container management — pass-through to the embedded Podman CLI",
		Long: `Thin pass-through to the embedded Podman binary (built from the
external/podman submodule). Every verb, flag, and output format
upstream podman supports is available — ycode does not reimplement
them. CONTAINER_HOST is auto-set to ycode's running engine socket so
containers + images land in the same store the rest of ycode uses.

ycode-specific extension commands:
  machine   Manage the embedded vfkit VM (ycode-owned; distinct from
            upstream` + " `podman machine` " + `which uses qemu)
  cleanup   Remove orphaned vfkit/gvproxy processes + stale sockets

For everything else, run` + " `ycode podman --help` " + `to see the upstream
help text, or consult the official docs at https://docs.podman.io/.

--use-system-binaries (or container.useSystem in settings.json)
defers to whatever podman is on $PATH instead of the embedded one.`,
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		SilenceUsage:       true, // upstream podman owns its own usage text
		RunE: func(cmd *cobra.Command, args []string) error {
			return execEmbeddedPodman(cmd.Context(), args)
		},
	}
	cmd.AddCommand(
		newPodmanMachineCmd(),
		newPodmanCleanupCmd(),
	)
	return cmd
}

// execEmbeddedPodman resolves the podman binary (embedded or
// system) and execs it with the caller's args, pointed at ycode's
// engine socket via CONTAINER_HOST. Stdin/stdout/stderr are
// inherited so streaming output (build progress, `logs -f`,
// interactive runs once -i/-t are wired) works as expected.
// Propagates the child's exit code via os.Exit so callers
// (Makefiles, CI scripts) see test failures as non-zero exits.
func execEmbeddedPodman(ctx context.Context, args []string) error {
	bin, err := resolvePodmanBinary()
	if err != nil {
		return err
	}
	cmd := buildPodmanExec(ctx, bin, container.DefaultSocketPath(), args)
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("podman: %w", runErr)
	}
	return nil
}

// buildPodmanExec constructs the exec.Cmd that ships args to the
// resolved podman binary. Split out from execEmbeddedPodman so tests
// can assert on the resulting command shape (binary path, args,
// CONTAINER_HOST env) without spawning a real process.
func buildPodmanExec(ctx context.Context, bin, socket string, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	env := os.Environ()
	if socket != "" {
		env = append(env, "CONTAINER_HOST=unix://"+socket)
	}
	cmd.Env = env
	return cmd
}

// resolvePodmanBinary picks the podman binary the pass-through
// should exec. Order:
//
//  1. --use-system-binaries → look up "podman" on $PATH (clean
//     error if missing — the operator asked for system mode
//     explicitly, fall-back to embed would silently violate that).
//  2. Embedded podman from internal/container/podman_embed —
//     extracted into the per-user cache on first use, re-validated
//     via sha256 on subsequent runs.
//  3. Fall back to a system $PATH lookup when no embed was built
//     (some dev builds skip the embed to save link time).
//  4. Hard error with a recipe pointing at `make podman-embed`.
func resolvePodmanBinary() (string, error) {
	if useSystemBinaries {
		bin, err := exec.LookPath("podman")
		if err != nil {
			return "", fmt.Errorf("podman not found on PATH (--use-system-binaries is set; clear it to use the embedded binary): %w", err)
		}
		return bin, nil
	}
	if podman_embed.Available() {
		bin, err := podman_embed.EnsurePodman(podmanCacheDir())
		if err != nil {
			return "", fmt.Errorf("extract embedded podman: %w", err)
		}
		return bin, nil
	}
	if bin, err := exec.LookPath("podman"); err == nil {
		return bin, nil
	}
	return "", fmt.Errorf("no embedded podman in this build and no podman on PATH — rebuild with `make podman-embed` or install upstream podman")
}

// podmanCacheDir is where the embedded binary self-extracts on
// first use. Per-user cache so the binary survives across ycode
// upgrades that don't change the embed hash; sha256-validated by
// EnsurePodman, so an upgrade with a new podman version transparently
// re-extracts.
func podmanCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ycode", "bin")
	}
	return filepath.Join(os.TempDir(), "ycode-bin")
}
