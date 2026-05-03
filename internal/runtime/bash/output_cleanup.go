package bash

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	// CleanupInterval is how often the background cleanup runs.
	CleanupInterval = 1 * time.Hour

	// OutputRetentionDuration is how long saved output files are kept.
	OutputRetentionDuration = 7 * 24 * time.Hour
)

// StartOutputCleanup launches a background goroutine that periodically removes
// saved output files older than the retention duration from the given directory.
// The goroutine exits when ctx is cancelled.
func StartOutputCleanup(ctx context.Context, outputDir string, logger *slog.Logger) {
	if outputDir == "" {
		return
	}

	go runCleanupLoop(ctx, outputDir, logger)
}

func runCleanupLoop(ctx context.Context, dir string, logger *slog.Logger) {
	// Run once immediately on startup.
	cleanOldOutputFiles(dir, logger)

	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanOldOutputFiles(dir, logger)
		}
	}
}

// cleanOldOutputFiles removes files in dir that are older than the retention period.
func cleanOldOutputFiles(dir string, logger *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory may not exist yet
	}

	cutoff := time.Now().Add(-OutputRetentionDuration)
	removed := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			if err := os.Remove(path); err == nil {
				removed++
			}
		}
	}

	if removed > 0 && logger != nil {
		logger.Info("cleaned old output files", "dir", dir, "removed", removed)
	}
}
