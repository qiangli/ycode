//go:build windows

package main

// weaveMaybeSetsid is a no-op on Windows; we don't have setsid
// semantics there, and the PTY path doesn't work either, so a
// backgrounded `ycode weave start` on Windows already has limited
// guarantees vs. its launching console.
func weaveMaybeSetsid(parentStdinTTY bool) {}
