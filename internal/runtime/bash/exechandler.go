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
)

// NewSecurityExecHandler creates an ExecHandler middleware that enforces
// permission mode and creates process groups for signal isolation.
// The killTimeout controls how long to wait between SIGTERM and SIGKILL.
func NewSecurityExecHandler(mode permission.Mode, killTimeout time.Duration) func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			hc := interp.HandlerCtx(ctx)

			// Validate the command against permission mode.
			binary := filepath.Base(args[0])
			if err := validateExecPermission(binary, args, mode); err != nil {
				fmt.Fprintln(hc.Stderr, err.Error())
				return interp.ExitStatus(126)
			}

			// Resolve path.
			path, err := interp.LookPathDir(hc.Dir, hc.Env, args[0])
			if err != nil {
				fmt.Fprintln(hc.Stderr, err)
				return interp.ExitStatus(127)
			}

			// Build the command with process group isolation.
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

			err = cmd.Start()
			if err == nil {
				// Forward context cancellation as SIGTERM→SIGKILL to process group.
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
			}

			switch e := err.(type) {
			case *exec.ExitError:
				if status, ok := e.Sys().(syscall.WaitStatus); ok && status.Signaled() {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return interp.ExitStatus(uint8(128 + status.Signal()))
				}
				return interp.ExitStatus(uint8(e.ExitCode()))
			case *exec.Error:
				fmt.Fprintf(hc.Stderr, "%v\n", e)
				return interp.ExitStatus(127)
			default:
				return err
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
