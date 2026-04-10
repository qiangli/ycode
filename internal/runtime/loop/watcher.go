package loop

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// FileWatcher watches a file for changes and triggers a callback.
type FileWatcher struct {
	path     string
	interval time.Duration
	callback func(content string) error
	logger   *slog.Logger
	lastMod  time.Time
}

// NewFileWatcher creates a watcher for a prompt file.
func NewFileWatcher(path string, interval time.Duration, callback func(string) error) *FileWatcher {
	return &FileWatcher{
		path:     path,
		interval: interval,
		callback: callback,
		logger:   slog.Default(),
	}
}

// Watch polls the file for changes until the context is canceled.
func (fw *FileWatcher) Watch(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(fw.interval):
			info, err := os.Stat(fw.path)
			if err != nil {
				continue
			}

			if info.ModTime().After(fw.lastMod) {
				fw.lastMod = info.ModTime()
				content, err := os.ReadFile(fw.path)
				if err != nil {
					fw.logger.Error("read watched file", "path", fw.path, "error", err)
					continue
				}
				if err := fw.callback(string(content)); err != nil {
					fw.logger.Error("watcher callback", "path", fw.path, "error", err)
				}
			}
		}
	}
}
