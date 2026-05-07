package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
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
			r.NotifyFileAccess(absPath)

			// Check for binary files.
			binary, err := fileops.IsBinaryFile(absPath)
			if err == nil && binary {
				return "File appears to be binary. Use bash to inspect it.", nil
			}

			// Check for sensitive files.
			action := fileops.CheckSensitiveFile(absPath)

			result, err := fileops.ReadFile(params)
			yotel.RecordFileop(ctx, "read", len(result), err == nil)
			if err != nil {
				return "", err
			}

			if action == fileops.FileAskUser {
				result = fmt.Sprintf("WARNING: This file (%s) may contain sensitive data (credentials, keys, etc.).\n\n%s", absPath, result)
			}

			return result, nil
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
			err = fileops.WriteFile(params, "")
			yotel.RecordFileop(ctx, "write", len(params.Content), err == nil)
			if err != nil {
				return "", err
			}
			r.NotifyFileWrite(params.Path)
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
			err = fileops.EditFile(params)
			// Edits don't carry a clean "bytes" semantic (replace string ↔ new
			// string of differing sizes); count the operation only.
			yotel.RecordFileop(ctx, "edit", 0, err == nil)
			if err != nil {
				return "", err
			}
			r.NotifyFileWrite(params.Path)
			return fmt.Sprintf("edited %s", params.Path), nil
		}
	}
}
