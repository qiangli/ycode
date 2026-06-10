// ycode-spawn — the micro PATH shim for `ycode wrap` sessions.
//
// Built standalone (stdlib only, ~1.6MB) and embedded into ycode via
// internal/runtime/wrap/spawn_embed; wrap materializes it into the
// per-session shim dir and points every tool symlink (bash, git,
// grep, …) at it. It resolves the real binary and exec(2)s — adding
// ~3ms and no resident memory, versus ~250ms and ~150MB resident
// when the shims pointed at the full ycode binary (the process
// fan-out behind the 2026-06-10 OOM).
//
// Build: scripts/embed-spawn.sh (invoked by `make compile`).
package main

import (
	"os"

	"github.com/qiangli/ycode/internal/runtime/spawncore"
)

func main() {
	os.Exit(spawncore.Dispatch(os.Args[0], os.Args[1:]))
}
