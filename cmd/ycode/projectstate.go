package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/projectid"
)

// migrateLegacyForemanDir copies commands.jsonl and state.json from
// <cwd>/.agents/ycode/foreman/ to the user-home foreman dir on first
// run. Idempotent: skipped if newDir already has either file.
//
// Like the backlog migration, legacy files are copied (not moved) and
// left in place for one release. After that, the in-repo dir is
// stale and the user can delete it.
func migrateLegacyForemanDir(oldDir, newDir string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if oldDir == "" || newDir == "" {
		return nil
	}
	// If the new dir already has either of the known foreman files, do nothing.
	for _, name := range []string{"commands.jsonl", "state.json"} {
		if _, err := os.Stat(filepath.Join(newDir, name)); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("foreman migrate: stat new %s: %w", name, err)
		}
	}
	// Are there any legacy files to migrate?
	srcEntries := 0
	for _, name := range []string{"commands.jsonl", "state.json"} {
		if _, err := os.Stat(filepath.Join(oldDir, name)); err == nil {
			srcEntries++
		}
	}
	if srcEntries == 0 {
		return nil
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return fmt.Errorf("foreman migrate: mkdir: %w", err)
	}
	copied := 0
	for _, name := range []string{"commands.jsonl", "state.json"} {
		src := filepath.Join(oldDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(newDir, name)); err != nil {
			return fmt.Errorf("foreman migrate: copy %s: %w", name, err)
		}
		copied++
	}
	log.Info("foreman: migrated legacy state",
		"from", oldDir,
		"to", newDir,
		"count", copied,
		"note", "legacy files left in place; new writes go only to user-home")
	return nil
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

// projectStateDir resolves the user-home, per-logical-project state
// directory: ~/.agents/ycode/projects/<sanitized-project-id>/.
//
// The id is computed from origin.Resolve precedence (explicit
// cfg.Project.ID in user-global settings.json > normalized git remote
// > cwd-hash fallback) so two checkouts of the same repo converge on
// the same directory. The directory is created on first use.
//
// All ycode-managed per-project state (backlog, foreman) lives under
// this dir. Settings have their own merge chain — see config.BootstrapLoader.
func projectStateDir(ctx context.Context) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	homeAgents := filepath.Join(home, ".agents", "ycode")
	userDir, err := os.UserConfigDir()
	if err != nil {
		userDir = filepath.Join(home, ".config")
	}
	userYcode := filepath.Join(userDir, "ycode")

	// peek only the user-global tier for Project.ID — see BootstrapLoader.
	_, id := config.BootstrapLoader(ctx, userYcode, homeAgents, cwd, cwd, cwd)
	if id == "" {
		return "", fmt.Errorf("could not resolve project id for %s", cwd)
	}
	dir := projectid.StateDir(homeAgents, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
