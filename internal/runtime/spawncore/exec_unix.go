//go:build !windows

package spawncore

import (
	"fmt"
	"os"
	"syscall"
)

// execReal replaces the current process image with the real binary.
// No fork, no wait, no babysitter: the shim's PID becomes the real
// tool, stdio and controlling terminal carry over, signals are
// delivered natively, and the shim's memory is fully released. The
// caller's exit-code observation is unchanged — the PID it waited on
// exits with the real tool's status.
func execReal(real string, args []string) int {
	argv := append([]string{real}, args...)
	if err := syscall.Exec(real, argv, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "ycode-spawn: exec %q: %v\n", real, err)
		return 126
	}
	return 0 // unreachable: Exec does not return on success
}
