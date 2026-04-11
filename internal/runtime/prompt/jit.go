package prompt

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JITDiscovery discovers instruction files in subdirectories when tools
// access file paths. Newly discovered files are collected and can be
// merged into the ProjectContext for the next API turn.
type JITDiscovery struct {
	mu          sync.Mutex
	projectRoot string          // upper bound for directory walking
	seen        map[string]bool // content hash → already known
	totalChars  int             // budget consumed so far (across startup + JIT)
	pending     []ContextFile   // newly discovered files not yet merged
}

// NewJITDiscovery creates a JIT discovery instance.
// projectRoot is the top of the project (e.g., git root) — discovery
// will not walk above this directory.
// existingSeen seeds the hash set from startup-discovered files so that
// JIT does not re-discover files already loaded.
func NewJITDiscovery(projectRoot string, existingSeen map[string]bool, existingChars int) *JITDiscovery {
	seen := make(map[string]bool, len(existingSeen))
	for k, v := range existingSeen {
		seen[k] = v
	}
	return &JITDiscovery{
		projectRoot: projectRoot,
		seen:        seen,
		totalChars:  existingChars,
	}
}

// OnToolAccess should be called when a tool accesses a file path.
// It discovers instruction files from the file's directory up to projectRoot.
// Returns the number of newly discovered files (0 if none).
func (j *JITDiscovery) OnToolAccess(accessedPath string) int {
	j.mu.Lock()
	defer j.mu.Unlock()

	dir := filepath.Dir(accessedPath)

	// Normalize projectRoot for comparison.
	projRoot, err := filepath.Abs(j.projectRoot)
	if err != nil {
		return 0
	}

	var newFiles []ContextFile

	for {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			break
		}

		// Stop if we've gone above the project root.
		if !isSubdirOrEqual(absDir, projRoot) {
			break
		}

		for _, name := range InstructionFileNames {
			path := filepath.Join(absDir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if len(content) == 0 {
				continue
			}

			hash := fmt.Sprintf("%x", sha256.Sum256(content))
			if j.seen[hash] {
				continue
			}
			j.seen[hash] = true

			text := string(content)
			if len(text) > MaxFileContentBudget {
				text = text[:MaxFileContentBudget] + "\n... (truncated)"
			}

			if j.totalChars+len(text) > MaxTotalBudget {
				break
			}
			j.totalChars += len(text)

			newFiles = append(newFiles, ContextFile{
				Path:    path,
				Content: text,
				Hash:    hash,
			})
		}

		// Move to parent directory.
		parent := filepath.Dir(absDir)
		if parent == absDir {
			break
		}
		dir = parent
	}

	j.pending = append(j.pending, newFiles...)
	return len(newFiles)
}

// DrainPending returns and clears all newly discovered files since the
// last drain. The caller should merge these into ProjectContext.ContextFiles.
func (j *JITDiscovery) DrainPending() []ContextFile {
	j.mu.Lock()
	defer j.mu.Unlock()

	files := j.pending
	j.pending = nil
	return files
}

// PendingCount returns the number of files waiting to be merged.
func (j *JITDiscovery) PendingCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.pending)
}

// isSubdirOrEqual checks if child is under (or equal to) parent.
func isSubdirOrEqual(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	// filepath.Rel returns ".." prefix if child is above parent.
	return rel == "." || (len(rel) > 0 && rel[0] != '.')
}
