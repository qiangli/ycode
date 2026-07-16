//go:build !windows

package shell

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

	"github.com/qiangli/ycode/internal/runtime/bash"
)

// PTYManager runs external commands that need a controlling terminal
// (vi, less, top, ssh, sudo, …). It implements bash.TTYRunner and is
// installed on the shell session via SetTTYRunner.
//
// In skeleton form: stdin/stdout/stderr are wired to os.Stdin/Stdout/Stderr.
// The terminal is briefly switched to raw mode while the child runs;
// SIGWINCH propagates the user's resizes to the PTY; ctx cancellation
// kills the child's process group with SIGTERM→SIGKILL escalation.
//
// The Bubble Tea TUI (Step 8) wraps PTYManager.RunTTY in a tea.ExecProcess
// callback so the Bubble Tea program suspends rendering during the handoff.
type PTYManager struct {
	// Stdin / Stdout / Stderr default to the os streams. Override for tests.
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
}

// NewPTYManager returns a PTYManager wired to the os streams.
func NewPTYManager() *PTYManager {
	return &PTYManager{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Compile-time check that PTYManager satisfies bash.TTYRunner.
var _ bash.TTYRunner = (*PTYManager)(nil)

// RunTTY runs argv attached to a freshly-created PTY. argv[0] is the
// already-resolved binary path. env and cwd are passed through to the
// child. Returns the child's exit code (0–255) or -1 on signal exit.
func (p *PTYManager) RunTTY(ctx context.Context, argv, env []string, cwd string) (int, error) {
	if len(argv) == 0 {
		return 1, errors.New("PTYManager: empty argv")
	}
	stdin := p.Stdin
	stdout := p.Stdout
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Dir = cwd
	// Do NOT set SysProcAttr.Setpgid here. creack/pty.StartWithSize adds
	// Setsid: true + Setctty: true unconditionally; on macOS the kernel
	// rejects fork/exec with EPERM when both Setpgid and Setsid are set.
	// Signals to the child still flow through the controlling terminal.
	// See internal/runtime/wrap/tty.go for the matching note in the wrap path.

	rows, cols := terminalSize(stdout)
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
				_ = pty.InheritSize(stdout, ptmx)
			case <-ctx.Done():
				return
			}
		}
	}()
	defer signal.Stop(winch)
	defer close(winch)

	// Switch the user's terminal to raw mode for the duration of the
	// child. If stdout isn't a terminal (test runs, headless smoke test),
	// skip — the child still gets its own PTY pair.
	var oldState *term.State
	if term.IsTerminal(int(stdout.Fd())) {
		oldState, err = term.MakeRaw(int(stdout.Fd()))
		if err != nil {
			// Non-fatal; carry on without raw mode.
			fmt.Fprintf(p.errStream(), "ycode shell: term.MakeRaw: %v\n", err)
		}
	}
	restoreOnce := sync.Once{}
	restore := func() {
		if oldState != nil {
			restoreOnce.Do(func() {
				_ = term.Restore(int(stdout.Fd()), oldState)
			})
		}
	}
	defer restore()

	// Stdin → PTY (one-way). Use a goroutine because os.Stdin reads block.
	go func() { _, _ = io.Copy(ptmx, stdin) }()
	// PTY → stdout. Block on this — when the child exits and closes
	// the slave, ptmx.Read returns io.EOF and we fall through.
	_, copyErr := io.Copy(stdout, ptmx)

	// Reap the child.
	waitErr := cmd.Wait()
	restore()

	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		// Best-effort: surface unexpected copy errors but don't override
		// the child's exit code.
		fmt.Fprintf(p.errStream(), "ycode shell: pty copy: %v\n", copyErr)
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

func (p *PTYManager) errStream() io.Writer {
	if p.Stderr == nil {
		return os.Stderr
	}
	return p.Stderr
}

// terminalSize returns rows, cols of the controlling terminal of `f`,
// falling back to 24×80 if the size can't be determined.
func terminalSize(f *os.File) (uint16, uint16) {
	if f == nil {
		return 24, 80
	}
	if w, h, err := term.GetSize(int(f.Fd())); err == nil {
		return uint16(h), uint16(w)
	}
	return 24, 80
}
