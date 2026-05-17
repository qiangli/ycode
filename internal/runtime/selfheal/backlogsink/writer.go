package backlogsink

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/qiangli/ycode/internal/gitserver/backlog"
)

// defaultWriter returns the production writer: backlog.WriteFile with
// an idempotency check against the on-disk file. Split into its own
// function so tests can inject a counting / failing writer without
// the real disk.
func defaultWriter(dir string) func(backlog.Issue) error {
	return func(issue backlog.Issue) error {
		if dir == "" {
			return fmt.Errorf("backlogsink: empty dir")
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("backlogsink: mkdir backlog dir: %w", err)
		}
		path := filepath.Join(dir, issue.Slug+".md")
		if _, err := os.Stat(path); err == nil {
			// Same signature has been captured in an earlier serve;
			// don't clobber operator edits or a worker's state
			// transitions.
			return errAlreadyExists
		}
		if err := backlog.WriteFile(issue, path); err != nil {
			return fmt.Errorf("backlogsink: write %s: %w", path, err)
		}
		return nil
	}
}

var errAlreadyExists = errors.New("backlogsink: entry already exists")

func isAlreadyExists(err error) bool {
	return errors.Is(err, errAlreadyExists)
}
