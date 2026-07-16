//go:build unix

package main

import "syscall"

// daemonSysProcAttr returns a SysProcAttr that starts the process in its
// own session (setsid), which decouples it from the controlling terminal.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
