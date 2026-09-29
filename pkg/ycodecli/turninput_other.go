//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly)

package ycodecli

import (
	"errors"
	"os"
)

// openTerminalKeys: mid-turn terminal ownership is implemented for Unix
// ttys; elsewhere a turn runs as before (typed lines wait for the prompt).
func openTerminalKeys(*os.File) (keySource, error) {
	return nil, errors.New("mid-turn terminal input is not supported on this platform")
}
