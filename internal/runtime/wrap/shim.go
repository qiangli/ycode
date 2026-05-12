package wrap

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// materializeShimDir creates a fresh, per-invocation directory of
// symlinks pointing at the running ycode binary, one entry per
// command name in commands. When the foreign agent shells out to one
// of those names through PATH, the kernel re-exec's ycode with
// argv[0]=basename — main() detects this via IsShimInvocation and
// dispatches to ShimMain.
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
func materializeShimDir(selfBinary string, commands []string) (string, error) {
	root := chooseShimRoot()
	dir := filepath.Join(root, "ycode-wrap", strconv.Itoa(os.Getpid()), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	abs, err := filepath.Abs(selfBinary)
	if err != nil {
		return "", fmt.Errorf("abs %s: %w", selfBinary, err)
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
		if err := os.Symlink(abs, dst); err != nil {
			// Symlink can fail on filesystems that don't support
			// them (e.g. some Windows mounts). Fall back to a copy.
			if cpErr := copyFile(abs, dst); cpErr != nil {
				return "", fmt.Errorf("symlink/copy %s: %w", dst, err)
			}
			// Make sure the copy is executable.
			_ = os.Chmod(dst, 0o755)
		}
	}
	return dir, nil
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
