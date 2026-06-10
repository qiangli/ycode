//go:build windows

package main

import (
	"fmt"
	"io"
	"os/exec"
)

// runWeaveToolPTY is a Unix-only feature; on Windows we surface a
// clear error and fall back to inherit-FD in the caller. Console
// host APIs differ enough (and the agentic CLIs targeted by weave
// are macOS/Linux-first) that supporting PTY on Windows isn't
// load-bearing for the MVP.
func runWeaveToolPTY(cmd *exec.Cmd, logSink io.Writer, guards weaveGuards) (int, error) {
	return 127, fmt.Errorf("weave: PTY not supported on Windows; pass --pty=never or run inside WSL")
}

func weaveStdinIsTTY() bool { return false }
