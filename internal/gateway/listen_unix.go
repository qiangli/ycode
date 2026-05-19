//go:build !windows

package gateway

import "net"

// listenPodman opens an AF_UNIX socket at path. The 0o700 mode is set
// on the directory in PodmanSocketPath; the socket itself inherits the
// usual umask, which is fine for loopback-only access on a single-user
// box. (Linux honours O_DIRECTORY/sticky bits — we don't need per-file
// chmod here.)
func listenPodman(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}
