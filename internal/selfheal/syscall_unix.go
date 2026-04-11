//go:build !windows

package selfheal

import (
	"fmt"
	"syscall"
)

// syscallExec wraps syscall.Exec with cwd change support.
// Note: syscall.Exec doesn't directly support changing cwd, so we use a workaround
// by setting the working directory via chdir before exec.
func syscallExec(argv0 string, argv []string, envv []string, cwd string) error {
	// Change to the target directory
	if err := syscall.Chdir(cwd); err != nil {
		return fmt.Errorf("failed to change directory: %w", err)
	}

	// Execute the new program, replacing current process
	return syscall.Exec(argv0, argv, envv)
}
