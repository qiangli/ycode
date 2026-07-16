//go:build windows

package bash

import (
	"fmt"
	"syscall"
)

// processGroupAttr returns a SysProcAttr for Windows.
// Windows does not have Unix-style process groups; the returned
// value keeps cmd.SysProcAttr untouched so process creation succeeds.
func processGroupAttr() *syscall.SysProcAttr {
	return nil
}

// killProcessGroup sends a signal to a process group.
// Windows does not support Unix-style process group signalling
// (negative-PID convention). Callers should use os.Process.Signal
// directly for single-process signalling instead.
func killProcessGroup(pgid int, sig syscall.Signal) error {
	return fmt.Errorf("process group signalling not supported on Windows (pgid=%d, sig=%v)", pgid, sig)
}
