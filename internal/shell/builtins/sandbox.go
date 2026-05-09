package builtins

import (
	"context"
	"fmt"
	"os/exec"
)

func init() { Register(&sandboxVerb{}) }

type sandboxVerb struct{}

func (sandboxVerb) Name() string { return "sandbox" }
func (sandboxVerb) Description() string {
	return "Run a command inside a podman sandbox (image: alpine; cwd mounted at /workspace)"
}
func (sandboxVerb) Usage() string { return "yc sandbox -- <command> [args…]" }

func (sandboxVerb) Run(ctx context.Context, args []string, stdio Stdio, cwd string) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc sandbox: missing command (use `--` to separate flags from the command)")
		return 2, nil
	}
	// Strip a leading "--" used by users to separate flags from command.
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc sandbox: missing command")
		return 2, nil
	}

	if _, err := exec.LookPath("podman"); err != nil {
		fmt.Fprintln(stdio.Stderr, "yc sandbox: podman not found in PATH")
		fmt.Fprintln(stdio.Stderr, "  install podman, or run from inside `ycode serve` (which embeds podman)")
		return 1, nil
	}

	// Best-effort minimal sandbox: alpine, cwd mounted read-write, no
	// network, no escalation. The container is removed on exit.
	cmdArgs := []string{
		"run", "--rm",
		"--network=none",
		"--volume", fmt.Sprintf("%s:/workspace:rw", cwd),
		"--workdir", "/workspace",
		"alpine:3.21",
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "podman", cmdArgs...)
	cmd.Stdin = stdio.Stdin
	cmd.Stdout = stdio.Stdout
	cmd.Stderr = stdio.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		fmt.Fprintf(stdio.Stderr, "yc sandbox: %v\n", err)
		return 1, nil
	}
	return 0, nil
}
