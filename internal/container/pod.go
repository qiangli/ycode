package container

import (
	"context"
	"fmt"
	"strings"
)

// PodOptions holds configuration for creating a pod.
type PodOptions struct {
	Name    string            // pod name
	Network string            // network name
	Labels  map[string]string // pod labels
}

// PodInfo holds information about a pod.
type PodInfo struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Status string `json:"Status"`
}

// CreatePod creates a new pod for grouping related agent containers.
// Containers in the same pod share a network namespace.
func (e *Engine) CreatePod(ctx context.Context, opts *PodOptions) (string, error) {
	args := []string{"pod", "create"}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Network != "" {
		args = append(args, "--network", opts.Network)
	}
	for k, v := range opts.Labels {
		args = append(args, "--label", k+"="+v)
	}

	out, err := e.Run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("create pod: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// StartPod starts all containers in a pod.
func (e *Engine) StartPod(ctx context.Context, nameOrID string) error {
	_, err := e.Run(ctx, "pod", "start", nameOrID)
	return err
}

// StopPod stops all containers in a pod.
func (e *Engine) StopPod(ctx context.Context, nameOrID string) error {
	_, err := e.Run(ctx, "pod", "stop", nameOrID)
	return err
}

// RemovePod removes a pod and all its containers.
func (e *Engine) RemovePod(ctx context.Context, nameOrID string, force bool) error {
	args := []string{"pod", "rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, nameOrID)
	_, err := e.Run(ctx, args...)
	return err
}

// ListPods lists pods matching the given name filter.
func (e *Engine) ListPods(ctx context.Context, nameFilter string) ([]PodInfo, error) {
	args := []string{"pod", "ls"}
	if nameFilter != "" {
		args = append(args, "--filter", "name="+nameFilter)
	}
	var pods []PodInfo
	if err := e.RunJSON(ctx, &pods, args...); err != nil {
		return nil, err
	}
	return pods, nil
}
