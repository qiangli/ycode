package builtins

import (
	"context"
	"fmt"

	container "github.com/qiangli/coreutils/external/podman/engine"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
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
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(stdio.Stderr, "yc sandbox: missing command")
		return 2, nil
	}

	eng, err := container.SharedEngine(ctx)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc sandbox: container engine unavailable: %v\n", err)
		return 1, nil
	}

	cfg := &container.ContainerConfig{
		Image:   "alpine:3.21",
		Network: "none",
		WorkDir: "/workspace",
		Mounts: []container.Mount{{
			Source: cwd,
			Target: "/workspace",
		}},
		Command: args,
	}

	ctx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeSandbox, "embedded-podman", append([]string{cfg.Image}, args...))

	result, err := eng.RunOneShot(ctx, cfg)
	exitCode := 0
	if result != nil {
		if _, werr := stdio.Stdout.Write([]byte(result.Stdout)); werr != nil {
			_ = werr
		}
		if result.Stdout != "" {
			fmt.Fprintln(stdio.Stdout)
		}
		if _, werr := stdio.Stderr.Write([]byte(result.Stderr)); werr != nil {
			_ = werr
		}
		if result.Stderr != "" {
			fmt.Fprintln(stdio.Stderr)
		}
		exitCode = result.ExitCode
	}
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc sandbox: %v\n", err)
		if exitCode == 0 {
			exitCode = 1
		}
	}
	finish(exitCode, err)
	return exitCode, nil
}
