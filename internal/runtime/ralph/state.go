package ralph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State holds the persistent state of a Ralph loop.
type State struct {
	Iteration       int      `json:"iteration"`
	Status          string   `json:"status"` // running, target_reached, stagnated, max_iterations, cancelled
	LastScore       float64  `json:"last_score"`
	LastOutput      string   `json:"last_output,omitempty"`
	LastCheckOutput string   `json:"last_check_output,omitempty"`
	LastError       string   `json:"last_error,omitempty"`
	Commits         []string `json:"commits,omitempty"`
}

// NewState creates an empty state.
func NewState() *State {
	return &State{
		Status: "pending",
	}
}

// Save persists the state to a JSON file.
func (s *State) Save(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadState reads state from a JSON file.
// Returns a new empty state if the file does not exist.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(), nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}
