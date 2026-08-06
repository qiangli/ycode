package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/projectid"
)

// projectStateDir resolves the user-home, per-logical-project state
// directory: ~/.agents/ycode/projects/<sanitized-project-id>/.
//
// The id is computed from origin.Resolve precedence (explicit
// cfg.Project.ID in user-global settings.json > normalized git remote
// > cwd-hash fallback) so two checkouts of the same repo converge on
// the same directory. The directory is created on first use.
//
// Ycode-managed per-project runtime state lives under this dir. Settings have
// their own merge chain — see config.BootstrapLoader.
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
