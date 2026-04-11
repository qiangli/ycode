package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// RegisterFileHandlers registers file tool handlers with VFS path validation.
func RegisterFileHandlers(r *Registry, v *vfs.VFS) {
	// read_file
	if spec, ok := r.Get("read_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.ReadFileParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse read_file input: %w", err)
			}
			absPath, err := v.ValidatePath(ctx, params.Path)
			if err != nil {
				return "", err
			}
			params.Path = absPath
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
			absPath, err := v.ValidatePath(ctx, params.Path)
			if err != nil {
				return "", err
			}
			params.Path = absPath
			// Pass empty workspace root since VFS already validated the path.
			if err := fileops.WriteFile(params, ""); err != nil {
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
			absPath, err := v.ValidatePath(ctx, params.Path)
			if err != nil {
				return "", err
			}
			params.Path = absPath
			if err := fileops.EditFile(params); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s", params.Path), nil
		}
	}
}
