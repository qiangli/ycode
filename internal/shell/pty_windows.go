//go:build windows

package shell

import (
	"context"
	"fmt"
	"os"

	"github.com/qiangli/ycode/internal/runtime/bash"
)

// PTYManager is a stub for Windows. PTY allocation is not supported on
// Windows because it requires Unix pseudo-terminal syscalls (posix_openpt,
// grantpt, etc.). The shell command uses TUITTYRunner on Windows instead.
type PTYManager struct {
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
}

// NewPTYManager returns a PTYManager stub.
func NewPTYManager() *PTYManager {
	return &PTYManager{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Compile-time check that PTYManager satisfies bash.TTYRunner.
var _ bash.TTYRunner = (*PTYManager)(nil)

// RunTTY always returns an error on Windows — PTY support is not available.
func (p *PTYManager) RunTTY(ctx context.Context, argv, env []string, cwd string) (int, error) {
	return 127, fmt.Errorf("PTY not available on Windows (use the TUI runner instead)")
}
