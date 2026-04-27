package todo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Save persists the board state to a JSON file.
func (b *Board) Save(path string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create todo dir: %w", err)
	}

	items := make([]*TodoItem, 0, len(b.items))
	for _, item := range b.items {
		items = append(items, item)
	}

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal todos: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// LoadBoard reads board state from a JSON file.
// Returns a new empty board if the file does not exist.
func LoadBoard(path string) (*Board, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewBoard(), nil
		}
		return nil, fmt.Errorf("read todos: %w", err)
	}

	var items []*TodoItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse todos: %w", err)
	}

	b := NewBoard()
	for _, item := range items {
		b.items[item.ID] = item
	}
	return b, nil
}
