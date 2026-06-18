//go:build !windows

package wrap

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// signalGraceWindow is how long the child gets between SIGTERM and the
// follow-up SIGKILL when the parent receives SIGINT/SIGTERM/SIGHUP.
// Mirrors the killTimeout used by internal/runtime/bash/exechandler.go,
// so agents that catch signals to flush state get a consistent window
// regardless of which wrapper invoked them.
const signalGraceWindow = 3 * time.Second

// forwardSignalsToChild installs handlers for SIGINT/SIGTERM/SIGHUP
// that forward each signal to the child's process group (Setpgid'd
// elsewhere) and escalate to SIGKILL after signalGraceWindow.
//
// Returns a stop closure the caller must invoke on Run() exit to
// release the signal handler and the goroutine. The returned closure
// is idempotent — calling it twice is a no-op.
func forwardSignalsToChild(cmd *exec.Cmd) func() {
	if cmd == nil || cmd.Process == nil {
		// Defensive — caller should call this after cmd.Start. Logging
		// at debug so a no-process path doesn't show up as a warning
		// in operator logs but is still discoverable when debugging.
		slog.Debug("wrap: forwardSignalsToChild called before Process attached")
		return func() {}
	}

	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	stop := make(chan struct{})
	var stopOnce sync.Once
	stopper := func() {
		stopOnce.Do(func() {
			signal.Stop(ch)
			close(stop)
		})
	}

	go func() {
		for {
			select {
			case sig := <-ch:
				if sig == nil {
					return
				}
				// Forward to the process group so any descendants the
				// agent spawned also get the signal — Setpgid makes
				// negative PID address the whole group on Unix.
				if usig, ok := sig.(syscall.Signal); ok {
					reapLeakedDescendants(os.Getpid())
					_ = signalProcessGroup(cmd.Process, usig)
					reapLeakedDescendants(os.Getpid())
				}
				if sig == syscall.SIGINT || sig == syscall.SIGTERM {
					// Schedule a SIGKILL escalation; the child may
					// exit before then, in which case the kill returns
					// ESRCH and we don't care.
					go func() {
						timer := time.NewTimer(signalGraceWindow)
						defer timer.Stop()
						select {
						case <-timer.C:
							reapLeakedDescendants(os.Getpid())
							_ = killProcessGroup(cmd.Process)
							reapLeakedDescendants(os.Getpid())
						case <-stop:
						}
					}()
				}
			case <-stop:
				return
			}
		}
	}()

	return stopper
}

func signalProcessGroup(proc *os.Process, sig syscall.Signal) error {
	if proc == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-proc.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func killProcessGroup(proc *os.Process) error {
	return signalProcessGroup(proc, syscall.SIGKILL)
}
