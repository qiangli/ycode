package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/computer"
	"github.com/qiangli/ycode/internal/runtime/fileops"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// RegisterFileHandlers registers file tool handlers backed by the
// Computer's Files surface. Path validation, sensitive-file
// detection, and binary-file detection remain in the handler; the
// raw I/O is delegated to the gateway.
func RegisterFileHandlers(r *Registry, files computer.Files) {
	// read_file
	if spec, ok := r.Get("read_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.ReadFileParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse read_file input: %w", err)
			}
			absPath, err := files.ValidatePath(ctx, params.Path)
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

			result, err := files.Read(ctx, params)
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
			err := files.Write(ctx, params)
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
			err := files.Edit(ctx, params)
			// Edits don't carry a clean "bytes" semantic; count the operation only.
			yotel.RecordFileop(ctx, "edit", 0, err == nil)
			if err != nil {
				return "", err
			}
			r.NotifyFileWrite(params.Path)
			return fmt.Sprintf("edited %s", params.Path), nil
		}
	}
}
