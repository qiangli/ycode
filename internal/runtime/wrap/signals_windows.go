//go:build windows

package wrap

import (
	"os"
	"os/exec"
)

// forwardSignalsToChild on Windows is a no-op. Ctrl-C handling on
// Windows is its own ecosystem (GenerateConsoleCtrlEvent, job
// objects, ...) and the wrap shim's value-add is observability, not
// signal politics. Returning a no-op stopper keeps the cross-platform
// call site identical to the Unix path.
func forwardSignalsToChild(_ *exec.Cmd) func() {
	return func() {}
}

func killProcessGroup(proc *os.Process) error {
	if proc == nil {
		return os.ErrProcessDone
	}
	return proc.Kill()
}
