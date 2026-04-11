//go:build windows

package selfheal

import (
	"fmt"
)

// syscallExec is not supported on Windows - we use a different approach.
// This function should never be called on Windows as restartWindows handles it differently.
func syscallExec(argv0 string, argv []string, envv []string, cwd string) error {
	return fmt.Errorf("syscall.Exec is not supported on Windows")
}
