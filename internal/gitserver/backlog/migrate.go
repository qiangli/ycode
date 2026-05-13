package backlog

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// MigrateLegacy copies *.md entries from a legacy in-repo backlog
// directory (typically <cwd>/docs/backlog/) to the user-home location
// (typically ~/.agents/ycode/projects/<id>/backlog/).
//
// Guards (idempotent — safe to call on every startup):
//   - oldDir must exist and contain at least one *.md file
//   - newDir must exist and contain zero *.md files
//
// When both guards pass, every *.md file is copied (not moved) and one
// Info-level deprecation notice is logged. The legacy files are left
// in place as a safety net for one release. Subsequent runs are no-ops
// because newDir is no longer empty.
//
// Subdirectories under oldDir are not traversed — the on-disk schema
// is flat (one file per slug).
func MigrateLegacy(oldDir, newDir string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if oldDir == "" || newDir == "" {
		return nil
	}

	oldEntries, err := mdEntries(oldDir)
	if err != nil {
		return fmt.Errorf("backlog migrate: scan old dir: %w", err)
	}
	if len(oldEntries) == 0 {
		return nil
	}

	newEntries, err := mdEntries(newDir)
	if err != nil {
		return fmt.Errorf("backlog migrate: scan new dir: %w", err)
	}
	if len(newEntries) > 0 {
		return nil
	}

	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("backlog migrate: mkdir new dir: %w", err)
	}
	copied := 0
	for _, name := range oldEntries {
		if err := copyFile(filepath.Join(oldDir, name), filepath.Join(newDir, name)); err != nil {
			return fmt.Errorf("backlog migrate: copy %s: %w", name, err)
		}
		copied++
	}
	log.Info("backlog: migrated legacy entries",
		"from", oldDir,
		"to", newDir,
		"count", copied,
		"note", "legacy files left in place; new writes go only to the user-home location")
	return nil
}

// mdEntries lists *.md filenames directly under dir (non-recursive).
// Returns an empty slice if dir does not exist.
func mdEntries(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
