//go:build windows

package wrap

import "syscall"

// newProcessGroupAttr is a no-op on Windows. The signal-forwarding
// path uses syscall.Kill which is unimplemented under Windows, so
// putting the child in its own process group buys nothing here.
// Returning nil keeps cmd.SysProcAttr untouched.
func newProcessGroupAttr() *syscall.SysProcAttr {
	return nil
}
