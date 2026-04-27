package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/bash"
	"github.com/qiangli/ycode/internal/runtime/permission"
)

// RegisterBashHandler registers the bash tool handler.
// An optional executor can be provided to route commands through a container
// sandbox. When executor is nil, commands run directly on the host.
func RegisterBashHandler(r *Registry, workDir string, executor ...bash.Executor) {
	spec, ok := r.Get("bash")
	if !ok {
		return
	}

	var exec bash.Executor
	if len(executor) > 0 {
		exec = executor[0]
	}

	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params bash.ExecParams
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse bash input: %w", err)
		}
		if params.WorkDir == "" {
			params.WorkDir = workDir
		}

		// Safety analysis: validate the command against the current permission mode.
		// If the user explicitly approved this invocation via the permission
		// prompter, treat it as full-access (the registry already confirmed).
		mode := resolvePermissionMode(r)
		if IsPermissionApproved(ctx) {
			mode = permission.DangerFullAccess
		}
		if err := bash.ValidateForMode(params.Command, mode); err != nil {
			return "", fmt.Errorf("bash safety check: %w", err)
		}

		result, err := bash.ExecuteWith(ctx, exec, params)
		if err != nil {
			return "", err
		}

		output := result.Stdout
		if result.Stderr != "" {
			if output != "" {
				output += "\n"
			}
			output += result.Stderr
		}
		if result.ExitCode != 0 {
			output += fmt.Sprintf("\n(exit code: %d)", result.ExitCode)
		}
		if output == "" {
			output = "(no output)"
		}
		return output, nil
	}
}

// resolvePermissionMode returns the current permission mode from the registry,
// defaulting to WorkspaceWrite if no resolver is configured.
func resolvePermissionMode(r *Registry) permission.Mode {
	r.mu.RLock()
	resolver := r.permResolver
	r.mu.RUnlock()
	if resolver != nil {
		return resolver()
	}
	return permission.WorkspaceWrite
}
