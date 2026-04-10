package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/runtime/lsp"
)

// RegisterLSPHandler registers the LSP tool handler.
func RegisterLSPHandler(r *Registry, lspRegistry *lsp.ClientRegistry) {
	spec, ok := r.Get("LSP")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Action   string `json:"action"`
			FilePath string `json:"file_path"`
			Line     int    `json:"line,omitempty"`
			Col      int    `json:"col,omitempty"`
			Language string `json:"language,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse LSP input: %w", err)
		}

		language := params.Language
		if language == "" {
			language = detectLanguage(params.FilePath)
		}

		client, ok := lspRegistry.Get(language)
		if !ok {
			return "", fmt.Errorf("no LSP server configured for language %q (available: %v)",
				language, lspRegistry.Languages())
		}

		req := &lsp.Request{
			Action:   lsp.Action(params.Action),
			FilePath: params.FilePath,
			Line:     params.Line,
			Col:      params.Col,
			Language: language,
		}

		resp, err := lsp.Execute(client, req)
		if err != nil {
			return "", fmt.Errorf("LSP %s failed: %w", params.Action, err)
		}

		return lsp.FormatResponse(resp), nil
	}
}

// detectLanguage guesses the language from a file extension.
func detectLanguage(path string) string {
	extMap := map[string]string{
		".go":   "go",
		".py":   "python",
		".js":   "javascript",
		".ts":   "typescript",
		".tsx":  "typescript",
		".jsx":  "javascript",
		".rs":   "rust",
		".java": "java",
		".rb":   "ruby",
		".c":    "c",
		".cpp":  "cpp",
		".h":    "c",
	}

	for ext, lang := range extMap {
		if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
			return lang
		}
	}
	return ""
}
