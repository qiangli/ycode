package loom

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultLeasePath returns the canonical lease-store path for a given
// gitea data directory. Used by every caller that constructs a
// FileStore against the embedded Gitea on disk, so the path is computed
// in exactly one place.
func DefaultLeasePath(giteaDataDir string) string {
	return filepath.Join(giteaDataDir, "loom", "leases.json")
}

// DefaultSandboxRoot returns the canonical sandbox root path for a given
// gitea data directory. Symmetric helper to DefaultLeasePath.
func DefaultSandboxRoot(giteaDataDir string) string {
	return filepath.Join(giteaDataDir, "loom", "sandboxes")
}

// DefaultGiteaDataDir returns the gitea data directory used by
// `ycode serve` when run with default config. Out-of-process callers
// (Worker subprocesses, CLI tools) use this when they need the same
// path the serve runtime computed but don't have access to its config.
//
// Callers MAY override by reading the YCODE_GITEA_DATA_DIR env var
// before falling back to this default; helper exposed via
// DefaultGiteaDataDirWithEnv.
func DefaultGiteaDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("loom: locate home: %w", err)
	}
	return filepath.Join(home, ".agents", "ycode", "observability", "gitea"), nil
}

// DefaultGiteaDataDirWithEnv returns the gitea data directory preferring
// the YCODE_GITEA_DATA_DIR env var (set by the parent when spawning
// Worker subprocesses) and falling back to DefaultGiteaDataDir.
func DefaultGiteaDataDirWithEnv() (string, error) {
	if v := os.Getenv("YCODE_GITEA_DATA_DIR"); v != "" {
		return v, nil
	}
	return DefaultGiteaDataDir()
}
