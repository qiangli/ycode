package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/worker"
)

// RegisterWorkerHandlers registers all 9 Worker* tool handlers.
func RegisterWorkerHandlers(r *Registry, registry *worker.Registry) {
	registerWorkerCreate(r, registry)
	registerWorkerGet(r, registry)
	registerWorkerObserve(r, registry)
	registerWorkerResolveTrust(r, registry)
	registerWorkerAwaitReady(r, registry)
	registerWorkerSendPrompt(r, registry)
	registerWorkerRestart(r, registry)
	registerWorkerTerminate(r, registry)
	registerWorkerObserveCompletion(r, registry)
}

func registerWorkerCreate(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerCreate")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerCreate input: %w", err)
		}
		w := registry.Create(params.Name)
		data, _ := json.Marshal(w)
		return string(data), nil
	}
}

func registerWorkerGet(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerGet")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerGet input: %w", err)
		}
		w, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("worker %q not found", params.ID)
		}
		data, _ := json.Marshal(w)
		return string(data), nil
	}
}

func registerWorkerObserve(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerObserve")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID       string `json:"id"`
			Snapshot string `json:"snapshot"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerObserve input: %w", err)
		}
		w, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("worker %q not found", params.ID)
		}
		return fmt.Sprintf("Worker %s observed (state: %s)", w.ID, w.State), nil
	}
}

func registerWorkerResolveTrust(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerResolveTrust")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerResolveTrust input: %w", err)
		}
		boot := worker.NewBoot(&worker.Worker{ID: params.ID, State: worker.StateTrustRequired}, registry, nil)
		if err := boot.ResolveTrust(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("Trust resolved for worker %s", params.ID), nil
	}
}

func registerWorkerAwaitReady(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerAwaitReady")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerAwaitReady input: %w", err)
		}
		w, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("worker %q not found", params.ID)
		}
		boot := worker.NewBoot(w, registry, nil)
		if err := boot.AwaitReady(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("Worker %s is ready", params.ID), nil
	}
}

func registerWorkerSendPrompt(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerSendPrompt")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID     string `json:"id"`
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerSendPrompt input: %w", err)
		}
		w, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("worker %q not found", params.ID)
		}
		boot := worker.NewBoot(w, registry, nil)
		if err := boot.SendPrompt(ctx, params.Prompt); err != nil {
			return "", err
		}
		return fmt.Sprintf("Prompt sent to worker %s", params.ID), nil
	}
}

func registerWorkerRestart(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerRestart")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerRestart input: %w", err)
		}
		if err := registry.SetState(params.ID, worker.StateSpawning); err != nil {
			return "", err
		}
		return fmt.Sprintf("Worker %s restarted", params.ID), nil
	}
}

func registerWorkerTerminate(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerTerminate")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerTerminate input: %w", err)
		}
		if err := registry.Terminate(params.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Worker %s terminated", params.ID), nil
	}
}

func registerWorkerObserveCompletion(r *Registry, registry *worker.Registry) {
	spec, ok := r.Get("WorkerObserveCompletion")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse WorkerObserveCompletion input: %w", err)
		}
		w, ok := registry.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("worker %q not found", params.ID)
		}
		switch w.State {
		case worker.StateFinished:
			return fmt.Sprintf("Worker %s completed. Output: %s", params.ID, w.Output), nil
		case worker.StateFailed:
			return fmt.Sprintf("Worker %s failed: %s", params.ID, w.Error), nil
		default:
			return fmt.Sprintf("Worker %s still running (state: %s)", params.ID, w.State), nil
		}
	}
}
