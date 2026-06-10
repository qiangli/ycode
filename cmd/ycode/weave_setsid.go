//go:build !windows

package main

import (
	"log/slog"
	"syscall"
)

// weaveMaybeSetsid moves the current process into a new session
// when invoked non-interactively, so a backgrounded `ycode weave
// start ... &` survives SIGHUP from its launching shell. Skipped
// when the parent stdin is a TTY because a user at a terminal
// expects ^C to reach the foreground ycode (Setsid would detach us
// from the controlling terminal and break that).
//
// Errors are logged and ignored — we may already be a session
// leader (EPERM) on platforms where the parent forked us with
// setsid for some other reason; either way, the worst case is the
// child gets SIGHUP'd when the shell exits, the same behavior as
// before this helper existed. Refusing to start is the wrong call.
func weaveMaybeSetsid(parentStdinTTY bool) {
	if parentStdinTTY {
		return
	}
	if _, err := syscall.Setsid(); err != nil {
		slog.Debug("weave: Setsid failed (likely already a session leader)", "err", err)
	}
}
