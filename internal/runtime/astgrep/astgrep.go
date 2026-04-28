// Package astgrep provides a containerized ast-grep tool for structural
// code search using tree-sitter AST patterns. ast-grep enables queries
// that are impossible with regex — matching by code structure rather than text.
//
// Example patterns:
//
//	$A && $A()                        — guard-and-call patterns
//	func $NAME($$$PARAMS) error       — all functions returning error
//	if err != nil { return $$$REST }  — all error handling blocks
//	fmt.Println($$$)                  — all debug print statements
package astgrep

import (
	"context"
	"path/filepath"

	"github.com/qiangli/ycode/internal/container"
	"github.com/qiangli/ycode/internal/runtime/containertool"
)

const imageName = "ycode-astgrep:latest"

// SearchInput is the input to the containerized ast-grep tool.
type SearchInput struct {
	Pattern  string   `json:"pattern"`            // ast-grep pattern
	Language string   `json:"language"`            // language (go, python, typescript, etc.)
	Paths    []string `json:"paths,omitempty"`     // paths to search (relative to workspace)
	Rewrite  string   `json:"rewrite,omitempty"`   // optional rewrite pattern
	JSON     bool     `json:"json"`                // always true for machine-readable output
}

// SearchMatch is a single match from ast-grep.
type SearchMatch struct {
	File        string `json:"file"`
	Line        int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	MatchedCode string `json:"matched_code"`
	Rewritten   string `json:"rewritten,omitempty"`
}

// SearchOutput is the output from the containerized ast-grep tool.
type SearchOutput struct {
	Matches []SearchMatch `json:"matches"`
}

// Dockerfile for the ast-grep container.
// Uses a multi-stage build: install ast-grep from npm, then copy to alpine.
const dockerfile = `FROM node:22-alpine AS builder
RUN npm install -g @ast-grep/cli@latest

FROM alpine:3.21
RUN apk add --no-cache nodejs
COPY --from=builder /usr/local/lib/node_modules/@ast-grep /usr/local/lib/node_modules/@ast-grep
COPY --from=builder /usr/local/bin/ast-grep /usr/local/bin/ast-grep
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
WORKDIR /workspace
ENTRYPOINT ["/entrypoint.sh"]
`

// entrypoint.sh reads JSON input from stdin and runs ast-grep.
const entrypointScript = `#!/bin/sh
set -e

# Read input from /tmp/input.json
INPUT=$(cat /tmp/input.json)
PATTERN=$(echo "$INPUT" | sed -n 's/.*"pattern":"\([^"]*\)".*/\1/p')
LANGUAGE=$(echo "$INPUT" | sed -n 's/.*"language":"\([^"]*\)".*/\1/p')
REWRITE=$(echo "$INPUT" | sed -n 's/.*"rewrite":"\([^"]*\)".*/\1/p')

# Build ast-grep command
CMD="ast-grep run --pattern '$PATTERN' --lang $LANGUAGE --json"

if [ -n "$REWRITE" ]; then
    CMD="$CMD --rewrite '$REWRITE'"
fi

# Execute and output JSON
eval $CMD 2>/dev/null || echo '[]'
`

// NewTool creates the containerized ast-grep tool.
func NewTool(workspaceRoot string, engine *container.Engine) *containertool.Tool {
	return &containertool.Tool{
		Name:       "astgrep",
		Image:      imageName,
		Dockerfile: dockerfile,
		Sources: map[string]string{
			"entrypoint.sh": entrypointScript,
		},
		Mounts: []containertool.Mount{
			{Source: workspaceRoot, Target: "/workspace", ReadOnly: true},
		},
		Engine: engine,
	}
}

// Search runs an ast-grep structural search on the workspace.
func Search(ctx context.Context, workspaceRoot string, engine *container.Engine, input SearchInput) ([]SearchMatch, error) {
	if engine == nil {
		return nil, nil
	}

	input.JSON = true
	tool := NewTool(workspaceRoot, engine)

	// If paths are specified, make them relative to workspace.
	for i, p := range input.Paths {
		if filepath.IsAbs(p) {
			rel, err := filepath.Rel(workspaceRoot, p)
			if err == nil {
				input.Paths[i] = rel
			}
		}
	}

	var matches []SearchMatch
	if err := tool.RunJSON(ctx, input, &matches); err != nil {
		return nil, err
	}

	return matches, nil
}
