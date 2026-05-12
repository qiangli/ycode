package tools

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/qiangli/ycode/internal/runtime/todo"
)

// todoWriteInput is the payload shape expected by the TodoWrite tool —
// matches the deepagents `write_todos` schema (content/status/activeForm).
// Status is constrained at validate time, not in the struct tag, so we can
// produce a clear error message listing valid values.
type todoWriteInput struct {
	Todos []struct {
		Content    string `json:"content"`
		Status     string `json:"status"`
		ActiveForm string `json:"activeForm,omitempty"`
	} `json:"todos"`
}

// RegisterTodoHandler wires the TodoWrite tool to populate a shared
// *todo.Board (the same instance rendered into the dynamic prompt region
// via prompt.TodosSection). Replacement semantics — the model rewrites
// the whole list each turn, and the previous board is cleared.
//
// persistPath is the on-disk JSON file for cross-session resume; pass
// "" to skip persistence. Load is the caller's responsibility at session
// start (todo.LoadBoard).
//
// Mirrors the deepagents `write_todos` pattern: agent declares plan →
// board is re-injected into next turn → multi-step work stays coherent.
func RegisterTodoHandler(r *Registry, board *todo.Board, persistPath string) {
	spec, ok := r.Get("TodoWrite")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var p todoWriteInput
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse TodoWrite input: %w", err)
		}

		items := make([]*todo.TodoItem, 0, len(p.Todos))
		now := time.Now()
		for i, t := range p.Todos {
			if t.Content == "" {
				return "", fmt.Errorf("todos[%d]: content is required", i)
			}
			status, err := parseTodoStatus(t.Status)
			if err != nil {
				return "", fmt.Errorf("todos[%d]: %w", i, err)
			}
			items = append(items, &todo.TodoItem{
				ID:         contentHashID(t.Content),
				Title:      t.Content,
				ActiveForm: t.ActiveForm,
				Status:     status,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
		}

		board.Replace(items)

		if persistPath != "" {
			if err := board.Save(persistPath); err != nil {
				return "", fmt.Errorf("persist todo board: %w", err)
			}
		}

		return fmt.Sprintf("Wrote %d todo(s). Board re-injected into next turn.", len(items)), nil
	}
}

// parseTodoStatus normalizes the deepagents-style status strings into
// todo.Status. "completed" is accepted as an alias for "done" since both
// labels are widely used (deepagents uses "completed"; ycode's primitive
// uses "done").
func parseTodoStatus(s string) (todo.Status, error) {
	switch s {
	case "pending":
		return todo.StatusPending, nil
	case "in_progress":
		return todo.StatusInProgress, nil
	case "done", "completed":
		return todo.StatusDone, nil
	case "blocked":
		return todo.StatusBlocked, nil
	default:
		return "", fmt.Errorf("invalid status %q (valid: pending, in_progress, done, blocked)", s)
	}
}

// contentHashID derives a stable 8-char ID from a todo's content so
// repeated writes of the same content map to the same ID — keeps the
// board idempotent across multiple TodoWrite calls within a turn.
func contentHashID(content string) string {
	sum := sha1.Sum([]byte(content))
	return hex.EncodeToString(sum[:4])
}
