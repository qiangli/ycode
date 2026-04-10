package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TodoItem represents a task in the todo list.
type TodoItem struct {
	ID          string `json:"id"`
	Content     string `json:"content"`
	Status      string `json:"status"` // pending, in_progress, completed
	Priority    string `json:"priority,omitempty"`
}

// RegisterTodoHandler registers the TodoWrite tool handler.
func RegisterTodoHandler(r *Registry, workDir string) {
	spec, ok := r.Get("TodoWrite")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Todos []TodoItem `json:"todos"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TodoWrite input: %w", err)
		}

		path := filepath.Join(workDir, ".ycode-todos.json")
		data, err := json.MarshalIndent(params.Todos, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal todos: %w", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", fmt.Errorf("write todos: %w", err)
		}
		return fmt.Sprintf("Wrote %d todos to %s", len(params.Todos), path), nil
	}
}
