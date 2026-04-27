package loop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ModelCheckpoint tracks a model checkpoint for the self-improvement loop.
type ModelCheckpoint struct {
	Path      string    `json:"path"`      // path to model weights
	Iteration int       `json:"iteration"` // which improvement iteration produced this
	Score     float64   `json:"score"`     // evaluation score at this checkpoint
	CreatedAt time.Time `json:"created_at"`
}

// CheckpointManager manages model checkpoints with rollback support.
type CheckpointManager struct {
	dir         string
	checkpoints []ModelCheckpoint
}

// NewCheckpointManager creates a checkpoint manager.
func NewCheckpointManager(dir string) (*CheckpointManager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create checkpoint dir: %w", err)
	}
	cm := &CheckpointManager{dir: dir}
	_ = cm.load() // ignore error on first run
	return cm, nil
}

// Save records a new checkpoint.
func (cm *CheckpointManager) Save(cp ModelCheckpoint) error {
	cp.CreatedAt = time.Now()
	cm.checkpoints = append(cm.checkpoints, cp)
	return cm.persist()
}

// Best returns the checkpoint with the highest score.
func (cm *CheckpointManager) Best() *ModelCheckpoint {
	if len(cm.checkpoints) == 0 {
		return nil
	}
	best := &cm.checkpoints[0]
	for i := range cm.checkpoints {
		if cm.checkpoints[i].Score > best.Score {
			best = &cm.checkpoints[i]
		}
	}
	return best
}

// Latest returns the most recent checkpoint.
func (cm *CheckpointManager) Latest() *ModelCheckpoint {
	if len(cm.checkpoints) == 0 {
		return nil
	}
	return &cm.checkpoints[len(cm.checkpoints)-1]
}

// Rollback returns the best checkpoint for recovery after a regression.
func (cm *CheckpointManager) Rollback() *ModelCheckpoint {
	return cm.Best()
}

// All returns all checkpoints.
func (cm *CheckpointManager) All() []ModelCheckpoint {
	return cm.checkpoints
}

func (cm *CheckpointManager) persist() error {
	data, err := json.MarshalIndent(cm.checkpoints, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cm.dir, "checkpoints.json"), data, 0o644)
}

func (cm *CheckpointManager) load() error {
	data, err := os.ReadFile(filepath.Join(cm.dir, "checkpoints.json"))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &cm.checkpoints)
}
