//go:build experimental

package live

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:extension
var extensionFS embed.FS

// ExtractExtension copies the embedded extension tree to dst,
// creating dst if needed. Existing files are overwritten. Returns
// the absolute dst path so the caller can print it for the user.
func ExtractExtension(dst string) (string, error) {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	root := "extension"
	err := fs.WalkDir(extensionFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := extensionFS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		return "", fmt.Errorf("extract extension: %w", err)
	}
	abs, _ := filepath.Abs(dst)
	return abs, nil
}

// DefaultExtractDir is the one canonical location for the unpacked
// ycode-live extension: ~/Downloads/ycode-chrome-ext.
//
// One name, one place, every time. Users who want a different
// location pass `--dest` to `ycode browser setup live`. We pick
// ~/Downloads because:
//
//   - It's universally visible in every file manager (Finder on
//     macOS hides ~/.cache and ~/Library by default, blocking the
//     Chrome "Load unpacked" dialog from navigating there).
//   - Users expect extracted artifacts to land in Downloads.
//   - It's easy to remove later if the user uninstalls.
//
// On the very rare host without a ~/Downloads (minimal Linux,
// headless CI), ExtractExtension's MkdirAll creates it.
func DefaultExtractDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads", "ycode-chrome-ext")
}
