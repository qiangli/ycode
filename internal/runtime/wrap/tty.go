//go:build !windows

package wrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/creack/pty/v2"
	"golang.org/x/term"
)

// PTYMode selects how wrap.Run handles stdio for the wrapped agent.
type PTYMode string

const (
	// PTYAuto (default) — allocate a PTY when both stdin and stdout
	// are terminals; otherwise inherit FDs unchanged. The right
	// default for the wrap CLI: interactive sessions get a real
	// controlling terminal, headless invocations (claude -p "task"
	// piped from a script) keep pipe semantics.
	PTYAuto PTYMode = "auto"
	// PTYAlways — allocate a PTY even when stdin is piped. Useful for
	// TUIs that test for TTY-ness internally and refuse to render
	// otherwise; the trade-off is that the wrapped agent's stdin
	// becomes the PTY slave, so EOF / Ctrl-D handling changes.
	PTYAlways PTYMode = "always"
	// PTYNever — never allocate a PTY. Used by automated tests and
	// in workflows that explicitly want pipe behavior.
	PTYNever PTYMode = "never"
)

// ParsePTYMode normalizes a --pty flag value. Empty / unrecognized
// values fall back to auto with a warn (same shape as ParseExportMode).
func ParsePTYMode(flag string) PTYMode {
	switch PTYMode(flag) {
	case PTYAuto, PTYAlways, PTYNever:
		return PTYMode(flag)
	case "":
		return PTYAuto
	default:
		return PTYAuto
	}
}

// shouldAllocatePTY decides whether wrap.Run should route the wrapped
// agent through runUnderPTY (true) or the inherit-FD path (false).
//
// auto: both stdin and stdout must be terminals.
// always: yes regardless.
// never: no regardless.
//
// Decision is made against the opts.Stdin / opts.Stdout when set,
// otherwise the process's os.Stdin / os.Stdout. Tests inject custom
// pipes via Options and naturally hit the inherit-FD branch.
func shouldAllocatePTY(mode PTYMode, opts Options) bool {
	switch mode {
	case PTYNever:
		return false
	case PTYAlways:
		return true
	}
	stdin := os.Stdin
	stdout := os.Stdout
	if f, ok := opts.Stdin.(*os.File); ok && f != nil {
		stdin = f
	} else if opts.Stdin != nil {
		// A non-file reader (bytes.Buffer in tests) — not a TTY.
		return false
	}
	if f, ok := opts.Stdout.(*os.File); ok && f != nil {
		stdout = f
	} else if opts.Stdout != nil {
		return false
	}
	return term.IsTerminal(int(stdin.Fd())) && term.IsTerminal(int(stdout.Fd()))
}

// runUnderPTY launches bin/args attached to a freshly-allocated PTY
// with the given env and cwd. Forwards SIGWINCH so terminal resizes
// reach the wrapped agent's controlling terminal; switches the user's
// terminal to raw mode for the duration; restores on exit. Mirrors
// internal/shell/pty.go's PTYManager.RunTTY — kept local to wrap so
// the package doesn't pull in the full internal/shell graph.
//
// Returns the wrapped agent's exit code, or 130 when the child was
// signaled by ctx-cancel.
func runUnderPTY(ctx context.Context, bin string, args, env []string, cwd string) (int, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Setctty: false}

	rows, cols := terminalSize(os.Stdout)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return 127, fmt.Errorf("pty.Start: %w", err)
	}
	defer ptmx.Close()

	// Forward SIGWINCH so terminal resizes propagate into the PTY.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	winchDone := make(chan struct{})
	go func() {
		defer close(winchDone)
		for {
			select {
			case <-winch:
				_ = pty.InheritSize(os.Stdout, ptmx)
			case <-ctx.Done():
				return
			}
		}
	}()
	defer signal.Stop(winch)
	defer close(winch)

	// Switch the user's terminal to raw mode for the duration of the
	// child. If stdout isn't a terminal (test runs), skip — the
	// child still gets its own PTY pair.
	var oldState *term.State
	if term.IsTerminal(int(os.Stdout.Fd())) {
		oldState, err = term.MakeRaw(int(os.Stdout.Fd()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "ycode wrap: term.MakeRaw: %v\n", err)
		}
	}
	restoreOnce := sync.Once{}
	restore := func() {
		if oldState != nil {
			restoreOnce.Do(func() {
				_ = term.Restore(int(os.Stdout.Fd()), oldState)
			})
		}
	}
	defer restore()

	// Stdin → PTY (one-way). Goroutine because os.Stdin reads block.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	// PTY → Stdout. Block until the child closes the slave.
	_, copyErr := io.Copy(os.Stdout, ptmx)

	waitErr := cmd.Wait()
	restore()

	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		fmt.Fprintf(os.Stderr, "ycode wrap: pty copy: %v\n", copyErr)
	}

	switch e := waitErr.(type) {
	case nil:
		return 0, nil
	case *exec.ExitError:
		if status, ok := e.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				if ctx.Err() != nil {
					return 130, nil
				}
				return 128 + int(status.Signal()), nil
			}
			return status.ExitStatus(), nil
		}
		return 1, nil
	default:
		return 1, waitErr
	}
}

func terminalSize(f *os.File) (uint16, uint16) {
	if f == nil {
		return 24, 80
	}
	if w, h, err := term.GetSize(int(f.Fd())); err == nil {
		return uint16(h), uint16(w)
	}
	return 24, 80
}
