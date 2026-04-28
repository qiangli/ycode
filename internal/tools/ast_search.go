package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/runtime/astgrep"
)

// containerEngine is an optional container engine for ast-grep.
var containerEngine *container.Engine

// astSearchWorkDir is the workspace root for ast-grep searches.
var astSearchWorkDir string

// SetContainerEngine sets the container engine for ast_search.
func SetContainerEngine(engine *container.Engine, workDir string) {
	containerEngine = engine
	astSearchWorkDir = workDir
}

// RegisterASTSearchHandler registers the ast_search tool for structural
// code search using tree-sitter AST patterns.
func RegisterASTSearchHandler(r *Registry) {
	r.Register(&ToolSpec{
		Name: "ast_search",
		Description: "Structural code search using AST patterns (powered by ast-grep). " +
			"Search by code structure, not text — finds matches regardless of formatting. " +
			"Examples: '$A && $A()' finds guard-and-call, 'func $NAME($$$) error' finds error-returning functions. " +
			"Requires container engine (podman).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {
					"type": "string",
					"description": "ast-grep structural pattern. Use $VAR for single node, $$$VAR for multiple nodes."
				},
				"language": {
					"type": "string",
					"description": "Target language",
					"enum": ["go", "python", "javascript", "typescript", "rust", "java", "c", "cpp", "ruby"]
				},
				"path": {
					"type": "string",
					"description": "Optional path to limit search scope (relative to workspace)"
				},
				"rewrite": {
					"type": "string",
					"description": "Optional rewrite pattern for structural code transformation"
				}
			},
			"required": ["pattern", "language"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			if containerEngine == nil {
				return "ast_search is not available (container engine not initialized). " +
					"Use grep_search for text-based search instead.", nil
			}

			var params struct {
				Pattern  string `json:"pattern"`
				Language string `json:"language"`
				Path     string `json:"path"`
				Rewrite  string `json:"rewrite"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse ast_search input: %w", err)
			}

			searchInput := astgrep.SearchInput{
				Pattern:  params.Pattern,
				Language: params.Language,
				Rewrite:  params.Rewrite,
			}
			if params.Path != "" {
				searchInput.Paths = []string{params.Path}
			}

			matches, err := astgrep.Search(ctx, astSearchWorkDir, containerEngine, searchInput)
			if err != nil {
				return "", fmt.Errorf("ast_search: %w", err)
			}

			if len(matches) == 0 {
				return "No structural matches found.", nil
			}

			var sb strings.Builder
			for _, m := range matches {
				if m.Rewritten != "" {
					fmt.Fprintf(&sb, "%s:%d: %s -> %s\n", m.File, m.Line, m.MatchedCode, m.Rewritten)
				} else {
					fmt.Fprintf(&sb, "%s:%d: %s\n", m.File, m.Line, m.MatchedCode)
				}
			}
			return sb.String(), nil
		},
	})
}
