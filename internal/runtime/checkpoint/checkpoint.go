// Package checkpoint provides generic workflow checkpoint/resume.
// Any workflow executor that implements Checkpointable can persist
// its state and resume from the last checkpoint after a crash.
package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Checkpoint captures the complete state of a workflow at a point in time.
type Checkpoint struct {
	ID           string            `json:"id"`
	WorkflowID   string            `json:"workflow_id"`
	WorkflowType string            `json:"workflow_type"` // dag, autoloop, sprint, flow
	Phase        string            `json:"phase"`
	State        json.RawMessage   `json:"state,omitempty"`
	Outputs      map[string]string `json:"outputs,omitempty"` // completed node/phase outputs
	CreatedAt    time.Time         `json:"created_at"`
	Resumable    bool              `json:"resumable"`
}

// Checkpointable is implemented by workflow executors that support resume.
type Checkpointable interface {
	Checkpoint() (*Checkpoint, error)
	ResumeFrom(cp *Checkpoint) error
}

// Store persists and retrieves checkpoints.
type Store interface {
	Save(ctx context.Context, cp *Checkpoint) error
	Load(ctx context.Context, workflowID string) (*Checkpoint, error)
	List(ctx context.Context) ([]*Checkpoint, error)
	Delete(ctx context.Context, workflowID string) error
}

// FileStore is a file-system-backed checkpoint store.
type FileStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileStore creates a checkpoint store backed by the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Save persists a checkpoint to disk.
func (s *FileStore) Save(_ context.Context, cp *Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("create checkpoint dir: %w", err)
	}

	cp.CreatedAt = time.Now()
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	path := filepath.Join(s.dir, cp.WorkflowID+".json")
	return os.WriteFile(path, data, 0o644)
}

// Load retrieves the latest checkpoint for a workflow.
func (s *FileStore) Load(_ context.Context, workflowID string) (*Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, workflowID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	return &cp, nil
}

// List returns all checkpoints sorted by creation time (newest first).
func (s *FileStore) List(_ context.Context) ([]*Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}

	var cps []*Checkpoint
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}

		var cp Checkpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			continue
		}
		cps = append(cps, &cp)
	}

	sort.Slice(cps, func(i, j int) bool {
		return cps[i].CreatedAt.After(cps[j].CreatedAt)
	})

	return cps, nil
}

// Delete removes a checkpoint.
func (s *FileStore) Delete(_ context.Context, workflowID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.dir, workflowID+".json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
