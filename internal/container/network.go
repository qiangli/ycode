package container

import (
	"context"
	"fmt"
	"strings"
)

// CreateNetwork creates a bridge network for the ycode session.
func (e *Engine) CreateNetwork(ctx context.Context, name string) error {
	_, err := e.Run(ctx, "network", "create", name)
	if err != nil {
		// Ignore "already exists" errors.
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("create network %s: %w", name, err)
	}
	return nil
}

// RemoveNetwork removes a named network.
func (e *Engine) RemoveNetwork(ctx context.Context, name string) error {
	_, err := e.Run(ctx, "network", "rm", name)
	if err != nil {
		// Ignore "not found" errors.
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such") {
			return nil
		}
		return fmt.Errorf("remove network %s: %w", name, err)
	}
	return nil
}

// ListNetworks lists networks matching the given name filter.
func (e *Engine) ListNetworks(ctx context.Context, nameFilter string) ([]NetworkInfo, error) {
	args := []string{"network", "ls"}
	if nameFilter != "" {
		args = append(args, "--filter", "name="+nameFilter)
	}
	var networks []NetworkInfo
	if err := e.RunJSON(ctx, &networks, args...); err != nil {
		return nil, err
	}
	return networks, nil
}

// NetworkInfo holds information about a podman network.
type NetworkInfo struct {
	Name   string `json:"Name"`
	ID     string `json:"Id"`
	Driver string `json:"Driver"`
}

// HostGateway returns the hostname that containers use to reach the host.
// For Podman this is "host.containers.internal".
func (e *Engine) HostGateway() string {
	return "host.containers.internal"
}
