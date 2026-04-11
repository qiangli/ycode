package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// GhostSnapshot captures session state before compaction.
// It is never sent to the model — it exists for debugging, recovery, and
// post-mortem analysis of what was lost during compaction.
type GhostSnapshot struct {
	Timestamp       time.Time `json:"timestamp"`
	MessageCount    int       `json:"message_count"`
	EstimatedTokens int       `json:"estimated_tokens"`
	Summary         string    `json:"summary"`
	CompactedIDs    []string  `json:"compacted_ids,omitempty"`
	KeyFiles        []string  `json:"key_files,omitempty"`
	ActiveTopic     string    `json:"active_topic,omitempty"`
}

// SaveGhostSnapshot writes a ghost snapshot to the session's ghosts directory.
func SaveGhostSnapshot(sessionDir string, snap *GhostSnapshot) error {
	ghostDir := filepath.Join(sessionDir, "ghosts")
	if err := os.MkdirAll(ghostDir, 0o755); err != nil {
		return fmt.Errorf("create ghost dir: %w", err)
	}

	filename := fmt.Sprintf("%d.json", snap.Timestamp.UnixMilli())
	path := filepath.Join(ghostDir, filename)

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ghost: %w", err)
	}

	return os.WriteFile(path, data, 0o644)
}

// LoadLatestGhost reads the most recent ghost snapshot from the session directory.
// Returns nil, nil if no ghosts exist.
func LoadLatestGhost(sessionDir string) (*GhostSnapshot, error) {
	ghosts, err := ListGhosts(sessionDir)
	if err != nil {
		return nil, err
	}
	if len(ghosts) == 0 {
		return nil, nil
	}

	path := filepath.Join(sessionDir, "ghosts", ghosts[len(ghosts)-1])
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ghost: %w", err)
	}

	var snap GhostSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal ghost: %w", err)
	}

	return &snap, nil
}

// ListGhosts returns ghost snapshot filenames sorted chronologically (oldest first).
func ListGhosts(sessionDir string) ([]string, error) {
	ghostDir := filepath.Join(sessionDir, "ghosts")
	entries, err := os.ReadDir(ghostDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ghost dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
