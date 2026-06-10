//go:build windows

package spawncore

import "os"

// relaySignals on Windows: only Interrupt is meaningfully deliverable;
// the relay is best-effort there.
var relaySignals = []os.Signal{os.Interrupt}
