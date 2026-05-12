package wrap

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// reapStaleShimDirs walks <shimRoot>/ycode-wrap/*/ and removes
// per-PID directories whose PID is no longer alive. Called once at
// the top of Run() so a previous wrap session that crashed (SIGKILL,
// host reboot, OOM) doesn't leak under $XDG_RUNTIME_DIR forever.
//
// Errors are logged at debug and do not block the new session — a
// shim directory leak is annoying but not load-bearing, and we'd
// rather start the new wrap than block on cleanup of stale state.
func reapStaleShimDirs(shimRoot string) {
	root := filepath.Join(shimRoot, "ycode-wrap")
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("wrap: reaper: ReadDir failed", "root", root, "err", err)
		}
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			// Not a per-PID dir — leave it alone. Some other tool may
			// have written under ycode-wrap by mistake; reaping its
			// data is worse than ignoring.
			continue
		}
		if isProcessAlive(pid) {
			continue
		}
		stale := filepath.Join(root, e.Name())
		if err := os.RemoveAll(stale); err != nil {
			slog.Debug("wrap: reaper: RemoveAll failed", "path", stale, "err", err)
			continue
		}
		slog.Debug("wrap: reaped stale shim dir", "path", stale, "pid", pid)
	}
}

// isProcessAlive uses the os.FindProcess + Signal(0) idiom that works
// on Unix and falls through to "assume alive" on Windows (where Signal
// is unsupported and the reaper's correctness matters less because
// XDG_RUNTIME_DIR doesn't apply).
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 doesn't deliver anything; it just checks whether the
	// PID is reachable. ESRCH means the process is gone; EPERM means
	// it exists but we lack permission (still alive). Any other error
	// is treated as "uncertain → assume alive" so we don't reap a
	// running session belonging to another user.
	if err := p.Signal(syscall.Signal(0)); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return false
		}
		// Treat permission-denied as alive; treat any other error as
		// "uncertain" and also alive.
		return errors.Is(err, syscall.EPERM) || !errors.Is(err, syscall.ESRCH)
	}
	return true
}
