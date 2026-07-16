//go:build unix

package bash

import "syscall"

// processGroupAttr returns a SysProcAttr that puts the child process
// in its own process group so signals can be delivered to the whole
// process tree at once.
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends sig to the process group identified by pgid.
// On Unix this uses the negative-PID convention to address the group.
func killProcessGroup(pgid int, sig syscall.Signal) error {
	return syscall.Kill(-pgid, sig)
}
