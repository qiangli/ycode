//go:build windows

package main

import "syscall"

// daemonSysProcAttr returns a SysProcAttr for Windows.
// Windows does not have Unix-style session management (setsid).
// The background server launch path is not used on Windows.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return nil
}
