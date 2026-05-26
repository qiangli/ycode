//go:build !windows

package wrap_test

import (
	"os"
	"strings"
	"testing"
)

// TestMain isolates wrap E2E tests from any outer ycode-wrap context
// they may be launched from. Without this, running `go test` inside
// a shell that's already nested under `ycode wrap` leaks two pieces
// of state into the test runner:
//
//  1. YCODE_WRAP_DEPTH (recursion counter) and the other YCODE_WRAP_*
//     coordination vars — these flow through `append(os.Environ(), ...)`
//     into wrap.Options.Env. injectShimEnv resets DEPTH to "0" for the
//     child, but other YCODE_WRAP_* vars (notably SHIM_DIR pointing at
//     an outer shim) keep their inherited values, mis-directing the
//     child's shim-strip logic.
//
//  2. The outer wrap's shim directory on PATH — this causes
//     exec.LookPath("python3") to resolve back to a shim symlink, so
//     wrap.Run ends up exec'ing the outer shim instead of the real
//     python3. The shim then tries to strip the INNER shimDir from
//     PATH (because injectShimEnv overwrote SHIM_DIR), fails to find
//     itself in PATH, re-resolves python3 via the same outer shim,
//     and the recursion guard trips at depth 4.
//
// Clearing the outer state at process start gives every test a clean
// baseline. Equivalent to running `env -u YCODE_WRAP_* PATH=<stripped>
// go test` from the shell.
func TestMain(m *testing.M) {
	if outerShimDir := os.Getenv("YCODE_WRAP_SHIM_DIR"); outerShimDir != "" {
		if path := os.Getenv("PATH"); path != "" {
			parts := strings.Split(path, string(os.PathListSeparator))
			out := parts[:0]
			for _, p := range parts {
				if p != outerShimDir {
					out = append(out, p)
				}
			}
			_ = os.Setenv("PATH", strings.Join(out, string(os.PathListSeparator)))
		}
	}
	for _, v := range []string{
		"YCODE_WRAP_DEPTH",
		"YCODE_WRAP_SHIM",
		"YCODE_WRAP_SHIM_DIR",
		"YCODE_WRAP_AGENT",
	} {
		_ = os.Unsetenv(v)
	}
	os.Exit(m.Run())
}
