package builtins

import (
	"context"
	"fmt"
	"time"

	"mvdan.cc/sh/v3/interp"

	telotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// Handler returns an interp.ExecHandler middleware that intercepts
// `yc <verb> [args…]` invocations and dispatches to a registered Verb.
// Anything else falls through to the next handler (typically the
// shell-mode exec handler that does Setpgid + os/exec).
//
// Wire it onto the bash session via session.AddExecMiddleware before
// the first RunString call.
func Handler() func(interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	return func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
		return func(ctx context.Context, args []string) error {
			if len(args) == 0 || args[0] != "yc" {
				return next(ctx, args)
			}
			hc := interp.HandlerCtx(ctx)

			if len(args) < 2 {
				fmt.Fprintln(hc.Stderr, "yc: missing verb. Try `yc help`.")
				return interp.ExitStatus(2)
			}
			verbName := args[1]
			verb, ok := Lookup(verbName)
			if !ok {
				fmt.Fprintf(hc.Stderr, "yc: unknown verb %q. Try `yc help`.\n", verbName)
				return interp.ExitStatus(127)
			}

			stdio := Stdio{
				Stdin:  hc.Stdin,
				Stdout: hc.Stdout,
				Stderr: hc.Stderr,
			}
			start := time.Now()
			exit, err := verb.Run(ctx, args[2:], stdio, hc.Dir)
			telotel.RecordShellBuiltin(ctx, verbName, time.Since(start), exit)
			if err != nil {
				fmt.Fprintf(hc.Stderr, "yc %s: %v\n", verbName, err)
				if exit == 0 {
					exit = 1
				}
			}
			if exit != 0 {
				return interp.ExitStatus(uint8(exit))
			}
			return nil
		}
	}
}
