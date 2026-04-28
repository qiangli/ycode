package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/fileops"
	"github.com/qiangli/ycode/internal/runtime/vfs"
	"github.com/qiangli/ycode/internal/storage"
)

// codeSearchIndex is an optional Bleve index for natural-language code search fallback.
var codeSearchIndex storage.SearchIndex

const codeIndexName = "code"

// SetCodeSearchIndex sets the Bleve index used for natural-language grep fallback.
func SetCodeSearchIndex(idx storage.SearchIndex) {
	codeSearchIndex = idx
}

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
				r.NotifyFileAccess(absPath)
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
				r.NotifyFileAccess(absPath)
			}
			result, err := fileops.IndexedGrepSearch(params, codeSearchIndex)
			if err != nil {
				return "", err
			}

			// If ripgrep found no results and Bleve is available, try full-text search.
			noMatches := len(result.Files) == 0 && len(result.Matches) == 0
			if noMatches && codeSearchIndex != nil {
				maxResults := 20
				bleveResults, bleveErr := codeSearchIndex.Search(ctx, codeIndexName, params.Pattern, maxResults)
				if bleveErr == nil && len(bleveResults) > 0 {
					var lines []string
					for _, r := range bleveResults {
						path := r.Document.Metadata["path"]
						if path == "" {
							path = r.Document.ID
						}
						lines = append(lines, path)
					}
					return fmt.Sprintf("No regex matches. Full-text search results:\n%s", strings.Join(lines, "\n")), nil
				}
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
