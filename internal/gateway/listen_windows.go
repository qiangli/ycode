//go:build windows

package gateway

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// listenPodman opens a Windows named pipe at the given name (e.g.
// \\.\pipe\ycode-podman-12345). The pipe is loopback-only by default —
// non-administrators on the same machine can read/write, which matches
// the AF_UNIX behaviour on Linux/macOS.
func listenPodman(name string) (net.Listener, error) {
	return winio.ListenPipe(name, nil)
}
