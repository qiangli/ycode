package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/treesitter"
)

// ASTSearchDeps holds dependencies for the ast_search tool handler.
type ASTSearchDeps struct {
	WorkDir string
}

// RegisterASTSearchHandler registers the ast_search tool for structural
// code search using in-process tree-sitter AST patterns.
func RegisterASTSearchHandler(r *Registry, deps *ASTSearchDeps) {
	workDir := ""
	if deps != nil {
		workDir = deps.WorkDir
	}

	parser := treesitter.NewParser()

	r.Register(&ToolSpec{
		Name: "ast_search",
		Description: "Structural code search using AST patterns (powered by tree-sitter). " +
			"Search by code structure, not text — finds matches regardless of formatting. " +
			"Examples: '$A && $A()' finds guard-and-call, 'func $NAME($$$) error' finds error-returning functions. " +
			"Supports Go, Python, JavaScript, TypeScript, Rust, Java, C, Ruby.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"pattern": {
					"type": "string",
					"description": "Structural search pattern. Use $VAR for single node wildcard, $$$VAR for multiple nodes."
				},
				"language": {
					"type": "string",
					"description": "Target language",
					"enum": ["go", "python", "javascript", "typescript", "rust", "java", "c", "ruby"]
				},
				"path": {
					"type": "string",
					"description": "Optional path to limit search scope (relative to workspace)"
				},
				"rewrite": {
					"type": "string",
					"description": "Optional rewrite pattern for structural code transformation (not supported in this lean build)"
				}
			},
			"required": ["pattern", "language"]
		}`),
		AlwaysAvailable: false,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Pattern  string `json:"pattern"`
				Language string `json:"language"`
				Path     string `json:"path"`
				Rewrite  string `json:"rewrite"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", fmt.Errorf("parse ast_search input: %w", err)
			}

			if params.Rewrite != "" {
				return "", fmt.Errorf("ast_search rewrite is not available in this lean build")
			}

			if treesitter.IsSupported(params.Language) {
				matches, err := searchWithTreeSitter(ctx, parser, workDir, params.Pattern, params.Language, params.Path)
				if err == nil {
					return formatMatches(matches), nil
				}
				return "", err
			}

			return "", fmt.Errorf("unsupported language for ast_search: %s", params.Language)
		},
	})
}

func searchWithTreeSitter(ctx context.Context, parser *treesitter.Parser, workDir, pattern, language, searchPath string) ([]treesitter.Match, error) {
	root := workDir
	if searchPath != "" {
		root = filepath.Join(workDir, searchPath)
	}

	var allMatches []treesitter.Match

	// Determine file extension for the language.
	ext := langToExt(language)

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				base := filepath.Base(path)
				if base == ".git" || base == "node_modules" || base == "vendor" ||
					base == "priorart" || base == "external" {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if !strings.HasSuffix(path, ext) {
			return nil
		}

		relPath, _ := filepath.Rel(workDir, path)
		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		matches, err := treesitter.SearchText(ctx, parser, source, language, pattern, relPath)
		if err != nil {
			return nil // skip files that fail to parse
		}

		allMatches = append(allMatches, matches...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return allMatches, nil
}

func formatMatches(matches []treesitter.Match) string {
	if len(matches) == 0 {
		return "No structural matches found."
	}

	var sb strings.Builder
	for _, m := range matches {
		fmt.Fprintf(&sb, "%s:%d: %s\n", m.File, m.StartLine, m.MatchedCode)
	}
	return sb.String()
}

func langToExt(lang string) string {
	switch lang {
	case "go":
		return ".go"
	case "python":
		return ".py"
	case "javascript":
		return ".js"
	case "typescript":
		return ".ts"
	case "rust":
		return ".rs"
	case "java":
		return ".java"
	case "c":
		return ".c"
	case "ruby":
		return ".rb"
	default:
		return "." + lang
	}
}
