package wrap

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/qiangli/ycode/internal/runtime/wrap/spawn_embed"
)

// materializeShimDir creates a fresh, per-invocation directory of
// symlinks, one entry per command name in commands.
//
// Preferred target: the embedded ycode-spawn micro shim (~1.6MB,
// stdlib-only), extracted into the dir as `.ycode-spawn`. It resolves
// the real binary and exec(2)s — ~3ms per command, nothing resident.
//
// Fallback (embed absent, e.g. bare `go build` without
// -tags embed_spawn, or extraction failure): symlinks point at the
// running ycode binary itself. The kernel then re-exec's ycode with
// argv[0]=basename — main() detects this via IsShimInvocation and
// dispatches to ShimMain, which costs the monolith's ~250ms boot per
// command (the fan-out behind the 2026-06-10 OOM; avoid relying on
// this path for real sessions).
//
// Choice of root:
//   - $XDG_RUNTIME_DIR/ycode-wrap/<pid>/bin when set (tmpfs; auto
//     reaped on session end on Linux)
//   - $TMPDIR/ycode-wrap/<pid>/bin otherwise (macOS, BSD)
//   - /tmp/ycode-wrap/<pid>/bin as last resort
//
// The <pid> suffix keeps concurrent ycode wrap invocations from
// stomping each other and makes leftover dirs from crashed sessions
// easy to attribute.
//
// Returns (binDir, sessionDir, error). binDir is the PATH-prepended
// directory full of symlinks; sessionDir is the parent under which
// other per-session artefacts (runtime-hook payloads) live. Callers
// remove sessionDir on exit to clean up both at once.
func materializeShimDir(selfBinary string, commands []string) (string, string, error) {
	root := chooseShimRoot()
	sessionDir := filepath.Join(root, "ycode-wrap", strconv.Itoa(os.Getpid()))
	dir := filepath.Join(sessionDir, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	abs, err := filepath.Abs(selfBinary)
	if err != nil {
		return "", "", fmt.Errorf("abs %s: %w", selfBinary, err)
	}

	// Dispatch target for the symlinks: the micro shim when embedded,
	// the ycode binary otherwise. Extraction failure falls back
	// rather than failing the session — a slow shim beats no shim.
	target := abs
	if spawn_embed.Available() {
		spawner := filepath.Join(dir, ".ycode-spawn")
		if err := spawn_embed.ExtractTo(spawner); err != nil {
			slog.Warn("wrap: ycode-spawn extract failed; shims fall back to the ycode binary", "err", err)
		} else {
			target = spawner
		}
	}

	for _, name := range commands {
		// Skip obviously bogus names. A shim called "ycode" would
		// re-enter the binary at depth 0 forever.
		if name == "" || name == "ycode" {
			continue
		}
		dst := filepath.Join(dir, name)
		// Remove any leftover from a previous (same-pid?) crash so
		// Symlink doesn't fail with EEXIST.
		_ = os.Remove(dst)
		if err := os.Symlink(target, dst); err != nil {
			// Symlink can fail on filesystems that don't support
			// them (e.g. some Windows mounts). Fall back to a copy.
			if cpErr := copyFile(target, dst); cpErr != nil {
				return "", "", fmt.Errorf("symlink/copy %s: %w", dst, err)
			}
			// Make sure the copy is executable.
			_ = os.Chmod(dst, 0o755)
		}
	}
	return dir, sessionDir, nil
}

func chooseShimRoot() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		if info, err := os.Stat(r); err == nil && info.IsDir() {
			return r
		}
	}
	if t := os.TempDir(); t != "" {
		return t
	}
	return "/tmp"
}

// copyFile is the symlink fallback for filesystems that disallow them.
// Mode is set to 0o755 by the caller; we copy bytes only.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
