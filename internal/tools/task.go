package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/task"
)

// RegisterTaskHandlers registers Task tool handlers.
func RegisterTaskHandlers(r *Registry, registry *task.Registry) {
	registerTaskCreate(r, registry)
	registerTaskGet(r, registry)
	registerTaskList(r, registry)
	registerTaskUpdate(r, registry)
	registerTaskStop(r, registry)
	registerTaskOutput(r, registry)
}

func registerTaskCreate(r *Registry, registry *task.Registry) {
	spec, ok := r.Get("TaskCreate")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TaskCreate input: %w", err)
		}
		t := registry.Create(params.Description, func(ctx context.Context) (string, error) {
			return "Task placeholder execution", nil
		})
		data, _ := json.Marshal(t)
		return string(data), nil
	}
}

func registerTaskGet(r *Registry, registry *task.Registry) {
	spec, ok := r.Get("TaskGet")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TaskGet input: %w", err)
		}
		t, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("task %q not found", params.ID)
		}
		data, _ := json.Marshal(t)
		return string(data), nil
	}
}

func registerTaskList(r *Registry, registry *task.Registry) {
	spec, ok := r.Get("TaskList")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		tasks := registry.List()
		data, _ := json.Marshal(tasks)
		return string(data), nil
	}
}

func registerTaskUpdate(r *Registry, registry *task.Registry) {
	spec, ok := r.Get("TaskUpdate")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TaskUpdate input: %w", err)
		}
		_, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("task %q not found", params.ID)
		}
		return fmt.Sprintf("Message sent to task %s", params.ID), nil
	}
}

func registerTaskStop(r *Registry, registry *task.Registry) {
	spec, ok := r.Get("TaskStop")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TaskStop input: %w", err)
		}
		if err := registry.Stop(params.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task %s stopped", params.ID), nil
	}
}

func registerTaskOutput(r *Registry, registry *task.Registry) {
	spec, ok := r.Get("TaskOutput")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse TaskOutput input: %w", err)
		}
		t, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("task %q not found", params.ID)
		}
		if t.Output != "" {
			return t.Output, nil
		}
		if t.Error != "" {
			return fmt.Sprintf("Error: %s", t.Error), nil
		}
		return fmt.Sprintf("Task %s is still %s", params.ID, t.Status), nil
	}
}
