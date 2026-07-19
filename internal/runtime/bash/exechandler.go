package bash

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"

	"github.com/qiangli/ycode/internal/runtime/permission"
	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// NewSecurityExecHandler is retained as the composition of the two handlers it
// used to be: permission validation THEN a real fork. It exists so callers that
// want the old all-in-one behavior keep it, but the runtime now wires the two
// pieces separately with coreutilsshell.Handler() BETWEEN them (validate →
// in-process coreutils → fork), so a registered pure-Go tool never reaches the
// fork below. See NewSecurityValidateHandler + NewForkExecHandler.
func NewSecurityExecHandler(mode permission.Mode, killTimeout time.Duration) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	validate := NewSecurityValidateHandler(mode)
	fork := NewForkExecHandler(killTimeout)
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return validate(fork(next))
	}
}

// NewSecurityValidateHandler enforces permission mode, then DELEGATES to next.
// Delegating is the whole point: it lets coreutilsshell.Handler() (wired after
// this handler) resolve a registered pure-Go tool IN-PROCESS instead of the old
// behavior where this handler forked every command itself and the coreutils
// handler downstream was dead code.
func NewSecurityValidateHandler(mode permission.Mode) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)
			binary := filepath.Base(args[0])
			if err := validateExecPermission(binary, args, mode); err != nil {
				fmt.Fprintln(hc.Stderr, err.Error())
				return interp.ExitStatus(126)
			}
			return next(ctx, args)
		}
	}
}

// NewForkExecHandler forks the resolved system binary with process-group signal
// isolation. It is the TERMINAL handler — it does not call next; a command only
// reaches it after coreutilsshell.Handler() declined to serve it in-process.
// The killTimeout controls how long to wait between SIGTERM and SIGKILL.
func NewForkExecHandler(killTimeout time.Duration) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)
			binary := filepath.Base(args[0])

			// Resolve path.
			path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
			if err != nil {
				fmt.Fprintln(hc.Stderr, err)
				telotel.RecordExec(ctx, telotel.ExecScopeBash, binary, 0, 127, err)
				return interp.ExitStatus(127)
			}

			// Open a per-spawn span+metric. Closed in defer below
			// after Start/Wait completes, with the resolved exit
			// code + err so classification is exact.
			ctx, finish := telotel.StartExecSpan(ctx, telotel.ExecScopeBash, path, args)
			var (
				runErr   error
				exitCode int
			)
			defer func() { finish(exitCode, runErr) }()

			// Build the command with process group isolation.
			cmd := exec.Cmd{
				Path:        path,
				Args:        args,
				Env:         execEnvFromExpand(hc.Env),
				Dir:         hc.Dir,
				Stdin:       hc.Stdin,
				Stdout:      hc.Stdout,
				Stderr:      hc.Stderr,
				SysProcAttr: processGroupAttr(),
			}

			runErr = cmd.Start()
			if runErr == nil {
				// Forward context cancellation as SIGTERM→SIGKILL to process group.
				stopf := context.AfterFunc(ctx, func() {
					pgid := cmd.Process.Pid
					if killTimeout <= 0 {
						_ = killProcessGroup(pgid, syscall.SIGKILL)
						return
					}
					_ = killProcessGroup(pgid, syscall.SIGTERM)
					time.Sleep(killTimeout)
					_ = killProcessGroup(pgid, syscall.SIGKILL)
				})
				defer stopf()

				runErr = cmd.Wait()
			}

			switch e := runErr.(type) {
			case *exec.ExitError:
				exitCode = e.ExitCode()
				if status, ok := e.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return interp.ExitStatus(uint8(128 + status.Signal()))
				}
				return interp.ExitStatus(uint8(e.ExitCode()))
			case *exec.Error:
				fmt.Fprintf(hc.Stderr, "%v\n", e)
				exitCode = 127
				return interp.ExitStatus(127)
			default:
				return runErr
			}
		}
	}
}

// validateExecPermission checks whether a binary with the given args is allowed
// under the specified permission mode.
func validateExecPermission(binary string, args []string, mode permission.Mode) error {
	// Reconstruct command string for classification.
	command := binary
	if len(args) > 1 {
		for _, a := range args[1:] {
			command += " " + a
		}
	}

	return ValidateForMode(command, mode)
}

// execEnvFromExpand converts an expand.Environ into a []string for exec.Cmd.Env.
func execEnvFromExpand(env expand.Environ) []string {
	// The expand.Environ.Each method iterates over all variables.
	var pairs []string
	env.Each(func(name string, vr expand.Variable) bool {
		if vr.Exported {
			pairs = append(pairs, name+"="+vr.String())
		}
		return true
	})
	return pairs
}
