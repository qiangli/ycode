package ralph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ArchiveEntry represents a saved run.
type ArchiveEntry struct {
	Name      string
	Path      string
	CreatedAt time.Time
}

// ArchiveRun saves the current run's state and progress to an archive directory.
func ArchiveRun(runDir, archiveBaseDir, label string) (string, error) {
	timestamp := time.Now().Format("2006-01-02")
	archiveName := fmt.Sprintf("%s-%s", timestamp, sanitizeLabel(label))
	archivePath := filepath.Join(archiveBaseDir, archiveName)

	if err := os.MkdirAll(archivePath, 0o755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	// Copy key files.
	files := []string{"state.json", "progress.txt", "prd.json"}
	for _, name := range files {
		src := filepath.Join(runDir, name)
		dst := filepath.Join(archivePath, name)
		data, err := os.ReadFile(src)
		if err != nil {
			continue // skip missing files
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return "", fmt.Errorf("copy %s: %w", name, err)
		}
	}

	return archivePath, nil
}

// ListArchives returns archived runs sorted by creation time (newest first).
func ListArchives(archiveBaseDir string) ([]ArchiveEntry, error) {
	entries, err := os.ReadDir(archiveBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var archives []ArchiveEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		archives = append(archives, ArchiveEntry{
			Name:      e.Name(),
			Path:      filepath.Join(archiveBaseDir, e.Name()),
			CreatedAt: info.ModTime(),
		})
	}
	sort.Slice(archives, func(i, j int) bool {
		return archives[i].CreatedAt.After(archives[j].CreatedAt)
	})
	return archives, nil
}

// RestoreArchive copies archived state back to the run directory.
func RestoreArchive(archivePath, runDir string) error {
	files := []string{"state.json", "progress.txt", "prd.json"}
	for _, name := range files {
		src := filepath.Join(archivePath, name)
		dst := filepath.Join(runDir, name)
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("restore %s: %w", name, err)
		}
	}
	return nil
}

func sanitizeLabel(s string) string {
	var result []byte
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else if c == ' ' {
			result = append(result, '-')
		}
	}
	if len(result) > 50 {
		result = result[:50]
	}
	return string(result)
}
