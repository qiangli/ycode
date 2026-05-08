package bash

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"mvdan.cc/sh/v3/interp"
)

// TTYRunner is the optional callback the shell-mode exec handler invokes
// when an external command needs a controlling terminal (vi, less, top,
// ssh, etc.). The shell package supplies the concrete implementation
// (PTYManager) — keeping this interface here avoids an import cycle.
//
// argv is the resolved command (argv[0] is the binary path). env is the
// expanded environment. cwd is the working directory. The callback is
// expected to attach the child to a PTY, forward signals, and return the
// child's exit code (or interp.ExitStatus) on completion.
type TTYRunner interface {
	RunTTY(ctx context.Context, argv, env []string, cwd string) (int, error)
}

// NewShellExecHandler is the exec middleware for `ycode shell` mode. It
// keeps the process-group + SIGTERM→SIGKILL escalation that the agent-mode
// handler has, but skips the V01–V12 validators because in shell mode the
// user is the operator (permission.DangerFullAccess) and validators are
// agent-mode policy, not user-mode policy.
//
// If tty is non-nil and NeedsTTY(command) returns true, the command is
// delegated to tty.RunTTY instead of being exec'd through the in-process
// pipe. The caller is responsible for any terminal-state handoff (raw
// mode, etc.) — see internal/shell/pty.go.
//
// killTimeout controls how long to wait between SIGTERM and SIGKILL on
// context cancellation. 2 seconds matches the agent-mode default.
func NewShellExecHandler(killTimeout time.Duration, tty TTYRunner) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)

			path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
			if err != nil {
				fmt.Fprintln(hc.Stderr, err)
				return interp.ExitStatus(127)
			}

			// PTY escalation: if the resolved command is a TTY-requiring
			// program, hand off to the configured TTYRunner so it gets a
			// real controlling terminal.
			resolved := append([]string{path}, args[1:]...)
			if tty != nil && needsTTYArgs(resolved) {
				exit, err := tty.RunTTY(ctx, resolved, execEnvFromExpand(hc.Env), hc.Dir)
				if err != nil {
					return err
				}
				if exit != 0 {
					return interp.ExitStatus(uint8(exit))
				}
				return nil
			}

			cmd := exec.Cmd{
				Path:   path,
				Args:   args,
				Env:    execEnvFromExpand(hc.Env),
				Dir:    hc.Dir,
				Stdin:  hc.Stdin,
				Stdout: hc.Stdout,
				Stderr: hc.Stderr,
				SysProcAttr: &syscall.SysProcAttr{
					Setpgid: true,
				},
			}

			if err := cmd.Start(); err != nil {
				switch e := err.(type) {
				case *exec.Error:
					fmt.Fprintf(hc.Stderr, "%v\n", e)
					return interp.ExitStatus(127)
				default:
					return err
				}
			}

			stopf := context.AfterFunc(ctx, func() {
				pgid := cmd.Process.Pid
				if killTimeout <= 0 {
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
					return
				}
				_ = syscall.Kill(-pgid, syscall.SIGTERM)
				time.Sleep(killTimeout)
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			})
			defer stopf()

			err = cmd.Wait()
			switch e := err.(type) {
			case *exec.ExitError:
				if status, ok := e.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return interp.ExitStatus(uint8(128 + status.Signal()))
				}
				return interp.ExitStatus(uint8(e.ExitCode()))
			case nil:
				return nil
			default:
				return err
			}
		}
	}
}

// needsTTYArgs is the same heuristic as NeedsTTY but operates on the
// already-resolved argv. Keeps the TTY check cheap and AST-free.
func needsTTYArgs(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	// Re-use the public NeedsTTY function (it expects a command string,
	// not argv). The args may contain spaces; quoting is only needed for
	// string-based fallback heuristics, so we join naively here.
	return NeedsTTY(argvToCommand(argv))
}

// argvToCommand rebuilds a coarse command string from argv for NeedsTTY's
// AST-based + string-based heuristics. Whitespace-perfect quoting is not
// required because NeedsTTY only inspects the leading binary base name
// and a few subcommand patterns.
func argvToCommand(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	if len(argv) == 1 {
		return argv[0]
	}
	out := make([]byte, 0, 64)
	out = append(out, argv[0]...)
	for _, a := range argv[1:] {
		out = append(out, ' ')
		out = append(out, a...)
	}
	return string(out)
}
