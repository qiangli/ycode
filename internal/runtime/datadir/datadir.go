// Package datadir resolves the storage root a ycode process uses for its
// single-writer state (the bbolt KV store, the SQLite database, the vector and
// search indexes, and the memex graph) and the policy for what happens when
// that root is already held by another process.
//
// The default root — ~/.agents/ycode/projects/data — is HOST-GLOBAL. Every
// ycode on the machine wants it, and only one can have it: bbolt takes an
// exclusive file lock, and so does the graph. That is correct for a single
// interactive session and wrong for anything that fans out (weave workers,
// parallel evals, two terminals). The second process loses the race.
//
// Two knobs fix that:
//
//	YCODE_DATA_DIR   absolute storage root — the direct override
//	YCODE_HOME       alternate ~/.agents/ycode — root becomes <it>/projects/data
//
// Give each concurrent session a distinct YCODE_DATA_DIR and they stop
// contending entirely.
//
// The second half is failing LOUDLY when they do contend. ycode used to log a
// warning, run without a store, and exit 0 — so a degraded run was
// indistinguishable from a good one, which is exactly the failure mode that
// makes a fan-out look successful while producing nothing. Losing the lock is
// now an error unless the operator explicitly asked for a store-less run via
// --no-store / YCODE_NO_STORE=1.
package datadir

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Environment variables that steer storage-root resolution and lock policy.
const (
	// EnvDataDir is the absolute storage root override. Highest precedence.
	EnvDataDir = "YCODE_DATA_DIR"
	// EnvHome relocates the whole ~/.agents/ycode tree; the storage root
	// becomes <EnvHome>/projects/data.
	EnvHome = "YCODE_HOME"
	// EnvNoStore opts into an explicit store-less (degraded) run.
	EnvNoStore = "YCODE_NO_STORE"
	// EnvLockTimeout bounds how long to wait for the storage lock, as a Go
	// duration ("0" disables waiting).
	EnvLockTimeout = "YCODE_STORE_LOCK_TIMEOUT"
)

// DefaultLockTimeout is how long to wait for another process to release the
// storage lock before giving up. Long enough to ride out a peer's shutdown,
// short enough that a stuck holder is reported rather than waited on forever.
const DefaultLockTimeout = 5 * time.Second

// Resolve returns the storage root for this process.
//
// Precedence: YCODE_DATA_DIR > YCODE_HOME/projects/data > home/.agents/ycode/projects/data.
func Resolve(home string) string {
	return ResolveWith(home, os.Getenv)
}

// ResolveWith is Resolve with an injectable environment, for tests.
func ResolveWith(home string, getenv func(string) string) string {
	if dir := strings.TrimSpace(getenv(EnvDataDir)); dir != "" {
		return filepath.Clean(dir)
	}
	if h := strings.TrimSpace(getenv(EnvHome)); h != "" {
		return filepath.Join(filepath.Clean(h), "projects", "data")
	}
	return filepath.Join(home, ".agents", "ycode", "projects", "data")
}

// NoStore reports whether the operator explicitly asked for a store-less run.
// Only an explicit opt-in may downgrade a lock failure from fatal to a warning.
func NoStore() bool {
	return truthy(os.Getenv(EnvNoStore))
}

// LockTimeout returns how long to wait for the storage lock. Unparseable values
// fall back to DefaultLockTimeout; a negative value clamps to zero (no wait).
func LockTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(EnvLockTimeout))
	if raw == "" {
		return DefaultLockTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultLockTimeout
	}
	if d < 0 {
		return 0
	}
	return d
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
