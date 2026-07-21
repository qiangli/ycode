package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/runtime/datadir"
	"github.com/qiangli/ycode/pkg/memex/store"
)

// storeInitError turns a storage-open failure into an operator-actionable
// message, and — this is the point — into a NON-ZERO exit.
//
// ycode's persistent state (bbolt KV, SQLite, badger graph) is single-writer
// and, by default, host-global. A second concurrent ycode used to lose that
// lock, log a warning, run with no memory or graph, and exit 0. A fan-out of
// weave workers therefore reported success while producing nothing, and the
// only evidence was a warning line nobody reads.
//
// So: say what is locked, say who can have it, and say the two ways out —
// give this session its own root, or ask for a store-less run on purpose.
func storeInitError(dataDir string, waited time.Duration, err error) error {
	if errors.Is(err, store.ErrLocked) || isLockish(err) {
		return fmt.Errorf(
			"storage at %s is locked by another ycode process (waited %s).\n"+
				"  Concurrent sessions must not share one data directory. Either:\n"+
				"    • give this session its own store:  %s=/path/to/session-data ycode ...\n"+
				"      (or relocate the whole tree with %s=/path/to/agents-home)\n"+
				"    • or run without persistence on purpose:  ycode --no-store ...  (%s=1)\n"+
				"  underlying error: %w",
			dataDir, waited, datadir.EnvDataDir, datadir.EnvHome, datadir.EnvNoStore, err)
	}
	return fmt.Errorf(
		"init storage at %s failed: %w\n"+
			"  Set %s to a writable per-session directory, or pass --no-store to run without persistence",
		dataDir, err, datadir.EnvDataDir)
}

// isLockish reports whether an error text looks like single-writer contention
// from a backend that does not export a sentinel (badger reports its directory
// lock as a plain formatted error).
func isLockish(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"another process is using this badger database",
		"cannot acquire directory lock",
		"resource temporarily unavailable",
		"timeout",
		"locked",
		"lock",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
