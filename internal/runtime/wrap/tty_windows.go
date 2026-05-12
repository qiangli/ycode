//go:build windows

package wrap

import (
	"context"
	"errors"
	"os"
)

// PTYMode is a no-op type on Windows — PTY allocation requires the
// ConPTY APIs which creack/pty does not yet expose in a way that
// composes with our exec.CommandContext model. Wrap on Windows uses
// inherit-FD exclusively today.
type PTYMode string

const (
	PTYAuto   PTYMode = "auto"
	PTYAlways PTYMode = "always"
	PTYNever  PTYMode = "never"
)

func ParsePTYMode(flag string) PTYMode { return PTYAuto }

func shouldAllocatePTY(_ PTYMode, _ Options) bool {
	return false
}

func runUnderPTY(_ context.Context, _ string, _, _ []string, _ string) (int, error) {
	return 1, errors.New("wrap: PTY allocation not supported on Windows; use --pty=never (default)")
}

// Reference os to avoid unused-import errors when the stub above
// changes.
var _ = os.Stdin
