package otel

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// CleanupOldFiles removes date-stamped files older than maxAge in the given directory.
// Files must match the pattern *-YYYY-MM-DD.* to be considered for cleanup.
func CleanupOldFiles(dir string, maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, e.Name())
			if err := os.Remove(path); err != nil {
				slog.Debug("retention: remove file", "path", path, "error", err)
			}
		}
	}
}

// StartRetentionCleanup runs periodic cleanup in a background goroutine.
// It cleans logs/, traces/, and metrics/ subdirectories under dataDir.
func StartRetentionCleanup(ctx context.Context, dataDir string, maxAge time.Duration) {
	// Run once immediately on startup.
	runCleanup(dataDir, maxAge)

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runCleanup(dataDir, maxAge)
			}
		}
	}()
}

func runCleanup(dataDir string, maxAge time.Duration) {
	subs := []string{"logs", "traces", "metrics"}

	// Clean legacy flat paths (backward compatibility).
	for _, sub := range subs {
		CleanupOldFiles(filepath.Join(dataDir, sub), maxAge)
	}

	// Clean per-instance subdirectories.
	instancesDir := filepath.Join(dataDir, "instances")
	instances, err := os.ReadDir(instancesDir)
	if err != nil {
		return // no instances dir yet
	}
	for _, inst := range instances {
		if !inst.IsDir() {
			continue
		}
		instDir := filepath.Join(instancesDir, inst.Name())
		for _, sub := range subs {
			CleanupOldFiles(filepath.Join(instDir, sub), maxAge)
		}
		// Remove empty instance dirs (all data expired).
		removeIfEmpty(instDir)
	}
}

// removeIfEmpty removes a directory if it contains no files (only empty subdirs).
func removeIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			return // has files, keep it
		}
		sub := filepath.Join(dir, e.Name())
		subEntries, err := os.ReadDir(sub)
		if err != nil || len(subEntries) > 0 {
			return // subdirectory is non-empty
		}
	}
	// All subdirs are empty (or dir is empty) — remove the tree.
	os.RemoveAll(dir)
}
