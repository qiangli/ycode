package batch

import (
	"encoding/json"
	"fmt"
	"os"
)

// Checkpoint tracks which prompts have been completed for resume.
type Checkpoint struct {
	CompletedIDs map[string]bool `json:"completed_ids"`
	path         string
}

// NewCheckpoint creates or loads a checkpoint.
func NewCheckpoint(path string) (*Checkpoint, error) {
	cp := &Checkpoint{
		CompletedIDs: make(map[string]bool),
		path:         path,
	}
	if path == "" {
		return cp, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cp, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &cp.CompletedIDs); err != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", err)
	}
	return cp, nil
}

// MarkCompleted records a prompt as done and saves.
func (cp *Checkpoint) MarkCompleted(id string) error {
	cp.CompletedIDs[id] = true
	return cp.Save()
}

// IsCompleted checks if a prompt was already processed.
func (cp *Checkpoint) IsCompleted(id string) bool {
	return cp.CompletedIDs[id]
}

// Save persists the checkpoint to disk.
func (cp *Checkpoint) Save() error {
	if cp.path == "" {
		return nil
	}
	data, err := json.Marshal(cp.CompletedIDs)
	if err != nil {
		return err
	}
	return os.WriteFile(cp.path, data, 0o644)
}
