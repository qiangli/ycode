//go:build !windows

package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty/v2"
	"golang.org/x/term"
)

// runWeaveToolPTY launches cmd attached to a freshly-allocated PTY.
//
// Stdin/stdout routing depends on the parent's terminal:
//   - parent stdin IS a TTY: switch to raw mode, bidirectionally
//     copy stdin↔PTY so the user types into the subagent's TUI and
//     sees it render normally. SIGWINCH propagates terminal resizes.
//   - parent stdin is NOT a TTY (orchestrator pipe, backgrounded
//     by shell &): logSink receives the PTY output verbatim and
//     subagent stdin is fed from /dev/null. The orchestrator's
//     pipes are not held open by us — the subagent thinks it has
//     a controlling terminal even though no human is attached.
//
// logSink is only used in the non-TTY path; pass nil for the TTY
// pass-through case. When idleTimeout > 0 we SIGTERM the subagent
// if no PTY output appears for that long — caught the claude-TUI
// stuck-mid-edit case in the dogfood. Returns the subagent's exit
// code (or 128+N when killed by signal N, matching the wrap helper).
func runWeaveToolPTY(cmd *exec.Cmd, logSink io.Writer, idleTimeout time.Duration) (int, error) {
	rows, cols := weavePTYSize()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return 127, fmt.Errorf("pty.Start: %w", err)
	}
	defer ptmx.Close()

	// Idle-timeout watchdog: tracks the time of the most recent PTY
	// write. A background goroutine SIGTERMs the subagent process
	// group if that timestamp stalls past idleTimeout. The watchdog
	// reads `idleDeadline` via atomic load; the copy loop bumps it
	// on every write.
	var lastWriteUnixNs atomic.Int64
	lastWriteUnixNs.Store(time.Now().UnixNano())
	watchdogStop := make(chan struct{})
	defer close(watchdogStop)
	if idleTimeout > 0 {
		go func() {
			ticker := time.NewTicker(idleTimeout / 4)
			defer ticker.Stop()
			for {
				select {
				case <-watchdogStop:
					return
				case <-ticker.C:
					last := time.Unix(0, lastWriteUnixNs.Load())
					if time.Since(last) >= idleTimeout {
						if cmd.Process != nil {
							slog.Warn("weave: subagent idle past --idle-timeout; sending SIGTERM",
								"pid", cmd.Process.Pid, "idle", time.Since(last), "timeout", idleTimeout)
							_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
						}
						return
					}
				}
			}
		}()
	}

	parentTTY := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))

	// Forward SIGWINCH so terminal resizes propagate into the subagent's
	// PTY. Even in the non-TTY path we install the handler — it costs
	// nothing and means a manual SIGWINCH (rare) still works.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	winchDone := make(chan struct{})
	go func() {
		defer close(winchDone)
		for range winch {
			if parentTTY {
				_ = pty.InheritSize(os.Stdout, ptmx)
			}
		}
	}()
	defer func() {
		signal.Stop(winch)
		close(winch)
		<-winchDone
	}()

	var (
		oldState    *term.State
		restoreOnce sync.Once
	)
	restore := func() {
		if oldState != nil {
			restoreOnce.Do(func() { _ = term.Restore(int(os.Stdout.Fd()), oldState) })
		}
	}
	defer restore()

	// activityTap wraps an io.Writer and bumps lastWriteUnixNs on
	// every successful write. The idle-timeout watchdog reads that
	// timestamp.
	bump := func(n int) {
		if n > 0 {
			lastWriteUnixNs.Store(time.Now().UnixNano())
		}
	}
	tap := func(w io.Writer) io.Writer { return &activityTap{w: w, bump: bump} }

	if parentTTY {
		// Raw mode so the user's keystrokes go straight to the
		// subagent's TTY. Goroutine for stdin→PTY (os.Stdin reads
		// block); PTY→stdout in the foreground (blocks until child
		// closes the slave).
		oldState, err = term.MakeRaw(int(os.Stdout.Fd()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "weave: term.MakeRaw: %v\n", err)
		}
		go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
		_, _ = io.Copy(tap(os.Stdout), ptmx)
	} else {
		// Non-TTY parent (orchestrator pipe / backgrounded by `cmd &`).
		// Subagent gets a PTY but stdin is closed; PTY output is
		// captured to logSink (typically a per-issue log file under
		// the queue dir). We deliberately do NOT copy to os.Stdout —
		// that would feed the subagent's TUI output back into the
		// orchestrator's pipe, the exact pattern that caused the
		// recent OOM incident.
		if logSink == nil {
			logSink = io.Discard
		}
		_, _ = io.Copy(tap(logSink), ptmx)
	}

	waitErr := cmd.Wait()
	restore()

	switch e := waitErr.(type) {
	case nil:
		return 0, nil
	case *exec.ExitError:
		if status, ok := e.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal()), nil
			}
			return status.ExitStatus(), nil
		}
		return 1, nil
	default:
		if errors.Is(waitErr, io.EOF) {
			return 0, nil
		}
		return 1, waitErr
	}
}

// activityTap wraps an io.Writer and calls bump(n) on each write,
// so the watchdog goroutine can detect a stalled subagent. The
// goroutine uses sync/atomic on the timestamp; the writer itself
// stays lock-free.
type activityTap struct {
	w    io.Writer
	bump func(int)
}

func (a *activityTap) Write(p []byte) (int, error) {
	n, err := a.w.Write(p)
	a.bump(n)
	return n, err
}

// weavePTYSize returns the controlling terminal's size, or 24x80 as
// a fallback so backgrounded subagents still get a sensible default.
func weavePTYSize() (uint16, uint16) {
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		return uint16(h), uint16(w)
	}
	return 24, 80
}

// weaveStdinIsTTY reports whether the calling process's stdin is a
// real terminal. Used to gate the auto-setsid + auto-log-file paths.
func weaveStdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
