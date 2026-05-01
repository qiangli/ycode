package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RegisterRuleHandler registers the CreateRule tool handler.
func RegisterRuleHandler(r *Registry, workDir string) {
	spec, ok := r.Get("CreateRule")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Name    string `json:"name"`
			Content string `json:"content"`
			Glob    string `json:"glob,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse CreateRule input: %w", err)
		}
		if params.Name == "" {
			return "", fmt.Errorf("name is required")
		}
		if params.Content == "" {
			return "", fmt.Errorf("content is required")
		}

		// Sanitize name to prevent path traversal.
		name := filepath.Base(params.Name)
		if !strings.HasSuffix(name, ".md") {
			name += ".md"
		}

		rulesDir := filepath.Join(workDir, ".agents", "ycode", "rules")
		if err := os.MkdirAll(rulesDir, 0o755); err != nil {
			return "", fmt.Errorf("create rules directory: %w", err)
		}

		// Build rule content with optional glob frontmatter.
		var content string
		if params.Glob != "" {
			content = fmt.Sprintf("---\nglob: %s\n---\n\n%s\n", params.Glob, params.Content)
		} else {
			content = params.Content + "\n"
		}

		rulePath := filepath.Join(rulesDir, name)
		if err := os.WriteFile(rulePath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write rule file: %w", err)
		}

		return fmt.Sprintf("Rule %q created at %s", params.Name, rulePath), nil
	}
}
