package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/vfs"
)

// RegisterVFSHandlers registers handlers for the VFS filesystem tools.
func RegisterVFSHandlers(r *Registry, v *vfs.VFS) {
	// copy_file
	if spec, ok := r.Get("copy_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Source      string `json:"source"`
				Destination string `json:"destination"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse copy_file input: %w", err)
			}
			if err := v.CopyFile(ctx, params.Source, params.Destination); err != nil {
				return "", err
			}
			return fmt.Sprintf("copied %s to %s", params.Source, params.Destination), nil
		}
	}

	// move_file
	if spec, ok := r.Get("move_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Source      string `json:"source"`
				Destination string `json:"destination"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse move_file input: %w", err)
			}
			if err := v.MoveFile(ctx, params.Source, params.Destination); err != nil {
				return "", err
			}
			return fmt.Sprintf("moved %s to %s", params.Source, params.Destination), nil
		}
	}

	// delete_file
	if spec, ok := r.Get("delete_file"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path      string `json:"path"`
				Recursive bool   `json:"recursive"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse delete_file input: %w", err)
			}
			if err := v.DeleteFile(ctx, params.Path, params.Recursive); err != nil {
				return "", err
			}
			return fmt.Sprintf("deleted %s", params.Path), nil
		}
	}

	// create_directory
	if spec, ok := r.Get("create_directory"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse create_directory input: %w", err)
			}
			if err := v.CreateDirectory(ctx, params.Path); err != nil {
				return "", err
			}
			return fmt.Sprintf("created directory %s", params.Path), nil
		}
	}

	// list_directory
	if spec, ok := r.Get("list_directory"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse list_directory input: %w", err)
			}
			return v.ListDirectory(ctx, params.Path)
		}
	}

	// tree
	if spec, ok := r.Get("tree"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path           string `json:"path"`
				Depth          int    `json:"depth"`
				FollowSymlinks bool   `json:"follow_symlinks"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse tree input: %w", err)
			}
			if params.Depth == 0 {
				params.Depth = 3
			}
			return v.Tree(ctx, params.Path, params.Depth, params.FollowSymlinks)
		}
	}

	// get_file_info
	if spec, ok := r.Get("get_file_info"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse get_file_info input: %w", err)
			}
			return v.GetFileInfo(ctx, params.Path)
		}
	}

	// read_multiple_files
	if spec, ok := r.Get("read_multiple_files"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Paths []string `json:"paths"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse read_multiple_files input: %w", err)
			}
			for _, p := range params.Paths {
				r.NotifyFileAccess(p)
			}
			return v.ReadMultipleFiles(ctx, params.Paths)
		}
	}

	// list_roots
	if spec, ok := r.Get("list_roots"); ok {
		spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
			return v.ListRoots(), nil
		}
	}
}
