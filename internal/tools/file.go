package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/fileops"
)

// RegisterFileHandlers registers file tool handlers.
func RegisterFileHandlers(r *Registry, workspaceRoot string) {
	// read_file
	if spec, ok := r.Get("read_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.ReadFileParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse read_file input: %w", err)
			}
			return fileops.ReadFile(params)
		}
	}

	// write_file
	if spec, ok := r.Get("write_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.WriteFileParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse write_file input: %w", err)
			}
			if err := fileops.WriteFile(params, workspaceRoot); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(params.Content), params.Path), nil
		}
	}

	// edit_file
	if spec, ok := r.Get("edit_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.EditFileParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse edit_file input: %w", err)
			}
			if err := fileops.EditFile(params); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s", params.Path), nil
		}
	}
}
