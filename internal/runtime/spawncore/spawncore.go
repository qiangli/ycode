// Package spawncore is the dispatch core shared by the ycode-spawn
// micro shim (cmd/ycode-spawn) and the in-binary fallback
// (wrap.ShimMain). It must import ONLY the standard library — the
// whole point of ycode-spawn is a ~1.6MB binary that boots in
// single-digit milliseconds, versus the ~250ms / ~150MB cost of
// booting the full ycode monolith per shimmed command (measured
// during the 2026-06-10 OOM post-mortem).
//
// Dispatch contract (mirrors the original wrap.ShimMain):
//  1. Refuse past the recursion-depth ceiling.
//  2. Strip the shim directory from $PATH so the real-binary lookup
//     does not re-hit the shim, and bump the depth counter for any
//     grandchildren that re-enter via $SHELL.
//  3. Resolve the real binary by argv[0] basename.
//  4. Fire one non-blocking datagram at the wrap parent's event
//     socket (telemetry; best-effort, never blocks the command).
//  5. exec(2) the real binary (unix) — the shim process image is
//     replaced, so nothing stays resident. Windows has no exec and
//     falls back to fork-and-wait.
package spawncore

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Environment variables coordinating the wrap session. The wrap
// parent (internal/runtime/wrap) injects these; shims consume them.
// Wire-format identifiers — do not rename.
const (
	EnvShim    = "YCODE_WRAP_SHIM"
	EnvDepth   = "YCODE_WRAP_DEPTH"
	EnvShimDir = "YCODE_WRAP_SHIM_DIR"
	EnvEvents  = "YCODE_WRAP_EVENTS"
	// EnvSpawnTrace opts the dispatch into fork-and-wait span mode:
	// "1" keeps the micro shim resident (~2MB) for the command's
	// duration so it can observe the exit code and emit a completion
	// event, from which the wrap parent reconstructs a real OTel
	// span. Anything else (default) means exec(2) — fastest, nothing
	// resident, spawn event only. Exit codes are only observable by
	// a process's parent on unix, so completion data costs a
	// babysitter by construction; this keeps it opt-in and tiny.
	EnvSpawnTrace = "YCODE_WRAP_SPAWN_TRACE"
)

// MaxDepth bounds shim re-entry (shim → $SHELL → shim → …).
const MaxDepth = 4

// SpawnEvent is the datagram payload sent to the wrap parent. Argv is
// deliberately excluded — command lines carry secrets; the tool name
// and depth are enough for rate/forensic accounting.
//
// Ev "spawn" fires before dispatch in every mode. Ev "exit" fires
// only in span mode (EnvSpawnTrace) after the child is reaped, with
// ExitCode and DurMs populated.
type SpawnEvent struct {
	Ev       string `json:"ev"` // "spawn" | "exit"
	Tool     string `json:"tool"`
	PID      int    `json:"pid"`
	PPID     int    `json:"ppid"`
	Depth    int    `json:"depth"`
	ExitCode *int   `json:"exit_code,omitempty"`
	DurMs    int64  `json:"dur_ms,omitempty"`
}

// Dispatch resolves and runs the real binary for argv0. On unix it
// returns only on failure (success replaces the process image via
// exec); on Windows it returns the child's exit code.
func Dispatch(argv0 string, args []string) int {
	base := filepath.Base(argv0)
	depth := envDepth()
	if depth >= MaxDepth {
		fmt.Fprintf(os.Stderr, "ycode-spawn: shim recursion depth %d exceeded for %q; refusing to dispatch\n", depth, base)
		return 125
	}

	shimDir := os.Getenv(EnvShimDir)
	_ = os.Setenv("PATH", StripPathEntry(os.Getenv("PATH"), shimDir))
	_ = os.Setenv(EnvDepth, strconv.Itoa(depth+1))

	real, err := exec.LookPath(base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ycode-spawn: real %q not found on PATH: %v\n", base, err)
		return 127
	}
	// Guard against LookPath resolving back into the shim dir despite
	// the strip — would loop via the kernel re-exec'ing the shim.
	if shimDir != "" {
		if rp, err := filepath.Abs(real); err == nil {
			if rp == shimDir || strings.HasPrefix(rp, shimDir+string(os.PathSeparator)) {
				fmt.Fprintf(os.Stderr, "ycode-spawn: real %q still inside shim dir %q after strip; refusing\n", base, shimDir)
				return 126
			}
		}
	}

	EmitSpawn(base, depth)
	if os.Getenv(EnvSpawnTrace) == "1" {
		return runRealWait(base, real, args, depth)
	}
	return execReal(real, args)
}

// runRealWait is the span-mode dispatch: fork-and-wait so the exit
// code and duration are observable, then emit the "exit" event the
// wrap parent turns into an OTel span. The babysitter here is the
// ~2MB micro shim, not the ycode monolith.
func runRealWait(tool, real string, args []string, depth int) int {
	start := time.Now()
	code := waitReal(real, args)
	emitExit(tool, depth, code, time.Since(start))
	return code
}

func emitExit(tool string, depth, exitCode int, dur time.Duration) {
	sock := os.Getenv(EnvEvents)
	if sock == "" {
		return
	}
	conn, err := net.DialTimeout("unixgram", sock, 5*time.Millisecond)
	if err != nil {
		return
	}
	defer conn.Close()
	payload, err := json.Marshal(SpawnEvent{
		Ev:       "exit",
		Tool:     tool,
		PID:      os.Getpid(),
		PPID:     os.Getppid(),
		Depth:    depth,
		ExitCode: &exitCode,
		DurMs:    dur.Milliseconds(),
	})
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Millisecond))
	_, _ = conn.Write(payload)
}

// EmitSpawn fires one fire-and-forget datagram at the wrap parent's
// event socket. Best-effort by design: no reply, a ~1ms write
// deadline, and every error ignored — telemetry must never slow down
// or fail the command being spawned.
func EmitSpawn(tool string, depth int) {
	sock := os.Getenv(EnvEvents)
	if sock == "" {
		return
	}
	conn, err := net.DialTimeout("unixgram", sock, 5*time.Millisecond)
	if err != nil {
		return
	}
	defer conn.Close()
	payload, err := json.Marshal(SpawnEvent{
		Ev:    "spawn",
		Tool:  tool,
		PID:   os.Getpid(),
		PPID:  os.Getppid(),
		Depth: depth,
	})
	if err != nil {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Millisecond))
	_, _ = conn.Write(payload)
}

// StripPathEntry removes every occurrence of dir from a PATH-style
// list. Empty dir returns path unchanged.
func StripPathEntry(path, dir string) string {
	if dir == "" {
		return path
	}
	parts := strings.Split(path, string(os.PathListSeparator))
	kept := parts[:0]
	for _, p := range parts {
		if p != dir {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

func envDepth() int {
	v := os.Getenv(EnvDepth)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}
