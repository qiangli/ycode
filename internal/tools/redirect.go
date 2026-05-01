// Package tools — redirect middleware intercepts suboptimal tool calls and
// suggests better alternatives. This is a soft redirect: the middleware returns
// an advisory message instead of executing, letting the LLM retry with the
// recommended tool. Hard redirects (silently swapping tools) are avoided to
// keep the LLM's mental model consistent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// RedirectRule defines when a tool call should be redirected.
type RedirectRule struct {
	// ToolName is the tool being intercepted.
	ToolName string

	// Match returns true if this specific invocation should be redirected.
	// Receives the parsed input JSON. Return false to let the call proceed normally.
	Match func(input json.RawMessage) bool

	// Suggest returns the advisory message shown to the LLM.
	Suggest func(input json.RawMessage) string
}

// defaultRedirectRules returns the built-in redirect rules.
func defaultRedirectRules() []RedirectRule {
	return []RedirectRule{
		// read_file on document formats → read_document
		{
			ToolName: "read_file",
			Match: func(input json.RawMessage) bool {
				path := extractStringField(input, "file_path")
				ext := strings.ToLower(filepath.Ext(path))
				switch ext {
				case ".pdf", ".docx", ".xlsx", ".pptx":
					return true
				}
				return false
			},
			Suggest: func(input json.RawMessage) string {
				path := extractStringField(input, "file_path")
				ext := strings.ToLower(filepath.Ext(path))
				return fmt.Sprintf(
					"The file %q is a %s document. Use the `read_document` tool instead of `read_file` — "+
						"it extracts text content from PDF, DOCX, XLSX, and PPTX files. "+
						"Call ToolSearch(query: \"select:read_document\") to activate it, then call read_document(file_path: %q).",
					filepath.Base(path), strings.ToUpper(strings.TrimPrefix(ext, ".")), path,
				)
			},
		},

		// read_file on large files without offset/limit → suggest chunked reading
		{
			ToolName: "read_file",
			Match: func(input json.RawMessage) bool {
				path := extractStringField(input, "file_path")
				if path == "" {
					return false
				}
				// Check if offset/limit are specified — if so, user is already chunking.
				var params struct {
					Offset *int `json:"offset"`
					Limit  *int `json:"limit"`
				}
				_ = json.Unmarshal(input, &params)
				if params.Offset != nil || params.Limit != nil {
					return false
				}
				// Check file size.
				size := getFileSize(path)
				return size > 500*1024 // 500KB threshold
			},
			Suggest: func(input json.RawMessage) string {
				path := extractStringField(input, "file_path")
				size := getFileSize(path)
				return fmt.Sprintf(
					"The file %q is large (%s). Reading it fully would exceed output limits and waste context. "+
						"Use `read_file` with `offset` and `limit` parameters to read specific sections, "+
						"or use `grep_search` to find relevant lines first. "+
						"Example: read_file(file_path: %q, offset: 0, limit: 100) to read the first 100 lines.",
					filepath.Base(path), formatBytes(size), path,
				)
			},
		},

		// bash with grep/rg for symbol search → suggest find_references or ast_search
		{
			ToolName: "bash",
			Match: func(input json.RawMessage) bool {
				cmd := extractStringField(input, "command")
				if cmd == "" {
					return false
				}
				lower := strings.ToLower(cmd)
				// Detect grep/rg used for symbol searching patterns.
				hasGrep := strings.Contains(lower, "grep ") || strings.Contains(lower, "rg ")
				hasSymbolPatterns := strings.Contains(lower, "func ") ||
					strings.Contains(lower, "class ") ||
					strings.Contains(lower, "def ") ||
					strings.Contains(lower, "interface ") ||
					strings.Contains(lower, "struct ")
				return hasGrep && hasSymbolPatterns
			},
			Suggest: func(input json.RawMessage) string {
				return "For searching code symbols (functions, classes, structs), use specialized tools instead of bash grep:\n" +
					"- `grep_search` — regex search with ripgrep, much faster and structured output\n" +
					"- `find_references` — finds all references to a symbol across the workspace using tree-sitter AST\n" +
					"- `ast_search` — structural code search using tree-sitter patterns\n" +
					"- `symbol_search` — search for symbol definitions by name\n" +
					"Call ToolSearch(query: \"code symbol references\") to activate these tools."
			},
		},

		// bash with find for file searching → suggest glob_search
		{
			ToolName: "bash",
			Match: func(input json.RawMessage) bool {
				cmd := extractStringField(input, "command")
				if cmd == "" {
					return false
				}
				lower := strings.ToLower(cmd)
				return strings.Contains(lower, "find ") && strings.Contains(lower, "-name ")
			},
			Suggest: func(input json.RawMessage) string {
				return "For finding files by name/pattern, use `glob_search` instead of `bash find`. " +
					"It is faster, respects .gitignore, and returns structured output. " +
					"Example: glob_search(pattern: \"**/*.go\", path: \"/project/src\")"
			},
		},

		// bash with cat/head/tail → suggest read_file
		{
			ToolName: "bash",
			Match: func(input json.RawMessage) bool {
				cmd := extractStringField(input, "command")
				if cmd == "" {
					return false
				}
				lower := strings.ToLower(cmd)
				return strings.HasPrefix(lower, "cat ") ||
					strings.HasPrefix(lower, "head ") ||
					strings.HasPrefix(lower, "tail ")
			},
			Suggest: func(input json.RawMessage) string {
				return "Use `read_file` instead of cat/head/tail. It provides line numbers, " +
					"handles large files with offset/limit pagination, detects binary files, " +
					"and warns about sensitive content. " +
					"Example: read_file(file_path: \"/path/to/file\", offset: 0, limit: 50)"
			},
		},

		// bash with sed/awk for file editing → suggest edit_file
		{
			ToolName: "bash",
			Match: func(input json.RawMessage) bool {
				cmd := extractStringField(input, "command")
				if cmd == "" {
					return false
				}
				lower := strings.ToLower(cmd)
				return (strings.HasPrefix(lower, "sed ") || strings.Contains(lower, "| sed ")) ||
					(strings.HasPrefix(lower, "awk ") || strings.Contains(lower, "| awk "))
			},
			Suggest: func(input json.RawMessage) string {
				return "Use `edit_file` instead of sed/awk for file modifications. " +
					"It provides exact string replacement with change tracking, " +
					"VFS boundary enforcement, and file write notifications. " +
					"Example: edit_file(file_path: \"/path\", old_string: \"before\", new_string: \"after\")"
			},
		},
	}
}

// ApplyRedirectMiddleware applies redirect rules to the registry.
// Rules are evaluated in order; the first matching rule short-circuits.
func ApplyRedirectMiddleware(r *Registry, rules []RedirectRule) {
	// Group rules by tool name for efficient dispatch.
	rulesByTool := make(map[string][]RedirectRule)
	for _, rule := range rules {
		rulesByTool[rule.ToolName] = append(rulesByTool[rule.ToolName], rule)
	}

	for toolName, toolRules := range rulesByTool {
		// Capture for closure.
		captured := toolRules
		err := r.ApplyMiddleware(toolName, func(next ToolFunc) ToolFunc {
			return func(ctx context.Context, input json.RawMessage) (string, error) {
				for _, rule := range captured {
					if rule.Match(input) {
						return rule.Suggest(input), nil
					}
				}
				return next(ctx, input)
			}
		})
		if err != nil {
			// Tool not registered — skip silently.
			_ = err
		}
	}
}

// extractStringField extracts a string field from JSON.
func extractStringField(input json.RawMessage, field string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// getFileSize returns file size in bytes, or 0 if the file can't be stat'd.
func getFileSize(path string) int64 {
	info, err := statFile(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// formatBytes formats a byte count as a human-readable string.
func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}
