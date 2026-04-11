package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// RegisterSearchHandlers registers glob and grep tool handlers with VFS path validation.
func RegisterSearchHandlers(r *Registry, v *vfs.VFS) {
	// glob_search
	if spec, ok := r.Get("glob_search"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.GlobParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse glob input: %w", err)
			}
			// Validate base path if provided.
			if params.Path != "" {
				absPath, err := v.ValidatePath(ctx, params.Path)
				if err != nil {
					return "", err
				}
				params.Path = absPath
			}
			result, err := fileops.GlobSearch(params)
			if err != nil {
				return "", err
			}
			if len(result.Files) == 0 {
				return "No files matched the pattern.", nil
			}
			return strings.Join(result.Files, "\n"), nil
		}
	}

	// grep_search
	if spec, ok := r.Get("grep_search"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params fileops.GrepParams
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse grep input: %w", err)
			}
			// Validate base path if provided.
			if params.Path != "" {
				absPath, err := v.ValidatePath(ctx, params.Path)
				if err != nil {
					return "", err
				}
				params.Path = absPath
			}
			result, err := fileops.GrepSearch(params)
			if err != nil {
				return "", err
			}

			switch params.OutputMode {
			case fileops.GrepOutputContent:
				var lines []string
				for _, m := range result.Matches {
					lines = append(lines, fmt.Sprintf("%s:%d: %s", m.File, m.Line, m.Content))
				}
				if len(lines) == 0 {
					return "No matches found.", nil
				}
				return strings.Join(lines, "\n"), nil
			case fileops.GrepOutputCount:
				return fmt.Sprintf("%d matches", result.Count), nil
			default: // files_with_matches
				if len(result.Files) == 0 {
					return "No matches found.", nil
				}
				return strings.Join(result.Files, "\n"), nil
			}
		}
	}
}
