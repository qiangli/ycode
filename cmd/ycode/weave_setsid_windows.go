//go:build windows

package main

// weaveMaybeSetsid is a no-op on Windows; we don't have setsid
// semantics there, and the PTY path doesn't work either, so a
// backgrounded `ycode weave start` on Windows already has limited
// guarantees vs. its launching console.
func weaveMaybeSetsid(parentStdinTTY bool) {}

// weaveStopWrapper on Windows is unimplemented for the MVP — the
// rest of the weave PTY/setsid path is unix-only too. Adding job-
// object based termination here would let `weave abandon` work on
// Windows but is deferred until someone actually needs it.
func weaveStopWrapper(pid int) {}
