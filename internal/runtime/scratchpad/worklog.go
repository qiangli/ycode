package scratchpad

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WorkLog is an append-only work log for narrative tracking.
type WorkLog struct {
	path string
}

// NewWorkLog creates a work log at the given directory.
func NewWorkLog(dir string) *WorkLog {
	return &WorkLog{path: filepath.Join(dir, "worklog.md")}
}

// Append adds an entry to the work log.
func (wl *WorkLog) Append(entry string) error {
	f, err := os.OpenFile(wl.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open worklog: %w", err)
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	_, err = fmt.Fprintf(f, "\n## %s\n%s\n", timestamp, entry)
	return err
}

// Read returns the full work log content.
func (wl *WorkLog) Read() (string, error) {
	data, err := os.ReadFile(wl.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}
