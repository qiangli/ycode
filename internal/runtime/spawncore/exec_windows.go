//go:build windows

package spawncore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// execReal on Windows: no exec(2), so fork-and-wait is the only
// option — but the babysitter is the ~2MB ycode-spawn process, not
// the ~150MB ycode monolith.
func execReal(real string, args []string) int {
	cmd := exec.Command(real, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "ycode-spawn: run %q: %v\n", real, err)
	return 1
}
