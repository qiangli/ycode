package bash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/qiangli/coreutils/pkg/telemetry"
	coreutilsshell "github.com/qiangli/coreutils/shell"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// Stdio bundles the per-RunString I/O streams. Any field may be nil:
// nil Stdin reads as EOF, nil Stdout/Stderr discard.
type Stdio struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// SetTTYRunner installs (or replaces) the TTYRunner used by the
// persistent shell-mode runner. Must be called before EnsureRunner /
// RunString to take effect; if the runner is already created, Reset()
// it first.
func (s *ShellSession) SetTTYRunner(tty TTYRunner) {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	s.tty = tty
}

// EnsureRunner lazily creates the persistent mvdan/sh Runner that backs
// `ycode shell`. The runner is reused across RunString calls so env,
// vars, functions, `set -e`/`set -u`/`set -o pipefail` flags, and
// aliases survive between submissions. (mvdan's Runner.Run only calls
// the internal Reset() on the first invocation; subsequent runs keep
// state.)
//
// Permission posture: NewShellExecHandler is installed (Setpgid + signal
// escalation, no validators). The shell-mode user is the operator at
// permission.DangerFullAccess.
func (s *ShellSession) EnsureRunner(_ context.Context) (*interp.Runner, error) {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	if s.runner != nil {
		return s.runner, nil
	}

	// THE PERSISTENT SESSION IS THE ONE THE SHELL TOOL ACTUALLY USES.
	//
	// ycode has TWO interpreters — this one and InterpreterExecutor — and wiring only the
	// other one produced exactly the symptom you would expect if the middleware were
	// broken: it was linked, it was correct, and it never fired. Two constructors of the
	// same thing is one more than can be kept in step.
	//
	// Outermost-first: telemetry (true wall-clock, final exit) → the shell/security gate
	// → the coreutils userland innermost, so a pure-Go tool resolves in-process and never
	// forks. This is bashy's chain; ycode now runs it too.
	mws := append([]func(interp.ExecHandlerFunc) interp.ExecHandlerFunc{}, telemetry.ExecMiddleware)
	mws = append(mws, s.extraExecMWs...)
	mws = append(mws, NewShellExecHandler(2*time.Second, s.tty), coreutilsshell.Handler())

	opts := []interp.RunnerOption{
		interp.Env(expand.ListEnviron(os.Environ()...)),
		interp.ExecHandlers(mws...),
	}
	if dir := s.WorkDir(); dir != "" {
		opts = append(opts, interp.Dir(dir))
	}
	runner, err := interp.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create persistent runner: %w", err)
	}
	s.runner = runner
	return runner, nil
}

// RunString parses src and runs it on the persistent Runner. Stdio is
// swapped in per-call via mvdan/sh's officially-supported pattern of
// re-applying interp.StdIO on an existing runner.
//
// Returns the program's exit code; only fatal interpreter errors come
// back as a non-nil error. Parse errors print to stderr and return 2.
func (s *ShellSession) RunString(ctx context.Context, src string, io Stdio) (int, error) {
	prog, perr := syntax.NewParser().Parse(strings.NewReader(src), "")
	if perr != nil {
		if io.Stderr != nil {
			fmt.Fprintf(io.Stderr, "parse error: %v\n", perr)
		}
		return 2, nil
	}

	runner, err := s.EnsureRunner(ctx)
	if err != nil {
		return 1, err
	}

	s.runnerMu.Lock()
	if err := interp.StdIO(io.Stdin, io.Stdout, io.Stderr)(runner); err != nil {
		s.runnerMu.Unlock()
		return 1, fmt.Errorf("install stdio: %w", err)
	}
	s.runnerMu.Unlock()

	runErr := runner.Run(ctx, prog)

	if newDir := runner.Dir; newDir != "" {
		s.SetWorkDir(newDir)
	}

	if runErr == nil {
		return 0, nil
	}
	var es interp.ExitStatus
	if errors.As(runErr, &es) {
		return int(es), nil
	}
	if ctx.Err() != nil {
		return 130, nil
	}
	return 1, runErr
}

// Reset drops the persistent runner. The next RunString call creates a
// fresh one with empty env/vars/funcs. Cwd survives because it is held
// on the session itself, not the runner.
func (s *ShellSession) Reset() {
	s.runnerMu.Lock()
	s.runner = nil
	s.runnerMu.Unlock()
}
