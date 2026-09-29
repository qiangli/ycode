//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package ycodecli

import "golang.org/x/sys/unix"

const (
	ioctlGetTermios = unix.TIOCGETA
	ioctlSetTermios = unix.TIOCSETA
)
