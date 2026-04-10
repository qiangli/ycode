package scratchpad

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Checkpoint represents a saved progress snapshot.
type Checkpoint struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Label     string    `json:"label,omitempty"`
	Data      any       `json:"data"`
}

// CheckpointManager manages progress snapshots.
type CheckpointManager struct {
	dir string
}

// NewCheckpointManager creates a new checkpoint manager.
func NewCheckpointManager(dir string) (*CheckpointManager, error) {
	cpDir := filepath.Join(dir, "checkpoints")
	if err := os.MkdirAll(cpDir, 0o755); err != nil {
		return nil, err
	}
	return &CheckpointManager{dir: cpDir}, nil
}

// Save persists a checkpoint.
func (cm *CheckpointManager) Save(id string, label string, data any) error {
	cp := Checkpoint{
		ID:        id,
		Timestamp: time.Now(),
		Label:     label,
		Data:      data,
	}
	bytes, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	path := filepath.Join(cm.dir, id+".json")
	return os.WriteFile(path, bytes, 0o644)
}

// Restore loads a checkpoint.
func (cm *CheckpointManager) Restore(id string) (*Checkpoint, error) {
	path := filepath.Join(cm.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", err)
	}
	return &cp, nil
}

// List returns all checkpoint IDs.
func (cm *CheckpointManager) List() ([]string, error) {
	entries, err := os.ReadDir(cm.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			ids = append(ids, e.Name()[:len(e.Name())-5])
		}
	}
	return ids, nil
}
