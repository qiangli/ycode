//go:build !windows

package wrap

import "syscall"

// newProcessGroupAttr returns a SysProcAttr that puts the child in its
// own process group so signal forwarding (forwardSignalsToChild) can
// address the entire descendant tree via Kill(-pgid, sig) on Unix.
//
// The Setpgid pattern is the same one internal/runtime/bash/exechandler.go
// uses for its bash-tool spawns — consistency across ycode's exec
// surfaces makes signal-handling behavior predictable for operators.
func newProcessGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
