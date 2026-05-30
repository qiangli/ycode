//go:build windows

package container

import "fmt"

// freeDiskBytes stub for Windows. The proper implementation uses
// GetDiskFreeSpaceEx from golang.org/x/sys/windows, but ycode podman
// isn't currently supported on Windows in earnest — log-and-skip
// rather than gate.
func freeDiskBytes(path string) (uint64, error) {
	return 0, fmt.Errorf("preflight disk check not implemented on windows")
}
