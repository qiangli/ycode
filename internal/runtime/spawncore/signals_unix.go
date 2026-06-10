//go:build !windows

package spawncore

import (
	"os"
	"syscall"
)

// relaySignals are forwarded from the waiting shim to its child in
// span mode. SIGINT usually also reaches the child via the shared
// foreground process group; the explicit relay covers signals aimed
// at the shim's PID directly (weave kill, scripted SIGTERM).
var relaySignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
