package treesitter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler exposes ycode's polyglot AST capabilities to external coding
// agents over the Model Context Protocol. All tools are read-only and run
// in-process; no CGO, no external services.
//
// Foreign agents typically pass `file_path` (resolved relative to the agent's
// cwd, which the spawned `ycode mcp serve` process inherits via stdio).
// `source` and `language` may be passed explicitly to override file reading
// and extension-based detection — useful for unsaved buffers or unfamiliar
// extensions.
type MCPHandler struct {
	parser *Parser
}

// NewMCPHandler constructs a handler with a fresh parser cache.
func NewMCPHandler() *MCPHandler {
	return &MCPHandler{parser: NewParser()}
}

// extToLang mirrors langAliases in languages.go but is kept locally so this
// file is the only place an MCP-facing tool touches.
var extToLang = map[string]string{
	".go":   "go",
	".py":   "python",
	".js":   "javascript",
	".jsx":  "javascript",
	".ts":   "typescript",
	".tsx":  "tsx",
	".rs":   "rust",
	".java": "java",
	".c":    "c",
	".h":    "c",
	".rb":   "ruby",
}

func languageFromPath(path string) string {
	return extToLang[strings.ToLower(filepath.Ext(path))]
}

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "list_symbols",
			Description: "Extract top-level symbols (functions, types, classes, methods, ...) from a single source file. " +
				"Best cold-start primitive for understanding what's defined where. Pass file_path; language is auto-detected " +
				"from the extension. Pass source to override file reading (useful for unsaved buffers). Returns a JSON array of Symbol objects.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_path": {"type": "string", "description": "Path to the source file (resolved relative to the MCP server's cwd)."},
					"source":    {"type": "string", "description": "Optional. If provided, parse this content instead of reading file_path."},
					"language":  {"type": "string", "description": "Optional. Override language detection (one of: go, python, javascript, typescript, tsx, rust, java, c, ruby)."}
				},
				"required": ["file_path"]
			}`),
		},
		{
			Name: "search_symbols_by_pattern",
			Description: "Search a source file for code matching an ast-grep-style pattern. Pattern syntax: literal code matches structurally; " +
				"$NAME matches any single AST node; $$$ matches zero or more nodes. Returns a JSON array of Match objects with line ranges " +
				"and matched code. Use list_symbols first if you don't know what to search for.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"file_path": {"type": "string"},
					"pattern":   {"type": "string", "description": "ast-grep-style pattern. Examples: 'func $NAME() {}', 'fmt.Printf($$$)', 'class $C extends $$$'."},
					"source":    {"type": "string", "description": "Optional. Override file reading."},
					"language":  {"type": "string", "description": "Optional. Override language detection."}
				},
				"required": ["file_path", "pattern"]
			}`),
		},
		{
			Name:        "get_supported_languages",
			Description: "List the languages this AST handler can parse. Returns a JSON array of canonical names. Use to discover capability scope before calling list_symbols on an unfamiliar file.",
			InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode declares each tool's permission tier. All AST tools are
// strictly read-only — they read a file from disk and parse it; no writes,
// no shell, no network.
func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "list_symbols":
		return h.handleListSymbols(ctx, input)
	case "search_symbols_by_pattern":
		return h.handleSearchByPattern(ctx, input)
	case "get_supported_languages":
		return h.handleSupportedLanguages()
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}

// resolveSource extracts (source, language) from common tool input shapes.
// It centralises the "explicit-source overrides file-read; explicit-language
// overrides extension-detection" rule so each tool handler stays a one-liner.
func resolveSource(filePath string, explicitSource, explicitLang string) ([]byte, string, error) {
	if filePath == "" {
		return nil, "", fmt.Errorf("file_path is required")
	}

	lang := explicitLang
	if lang == "" {
		lang = languageFromPath(filePath)
	}
	if lang == "" {
		return nil, "", fmt.Errorf("could not infer language from %q; pass language explicitly", filePath)
	}
	if !IsSupported(lang) {
		return nil, "", fmt.Errorf("unsupported language: %s", lang)
	}

	if explicitSource != "" {
		return []byte(explicitSource), lang, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", filePath, err)
	}
	return data, lang, nil
}

func (h *MCPHandler) handleListSymbols(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		FilePath string `json:"file_path"`
		Source   string `json:"source"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}

	source, lang, err := resolveSource(args.FilePath, args.Source, args.Language)
	if err != nil {
		return "", err
	}

	tree, err := h.parser.Parse(ctx, source, lang)
	if err != nil {
		return "", err
	}

	symbols := ExtractSymbols(tree, args.FilePath)
	out, err := json.Marshal(symbols)
	if err != nil {
		return "", fmt.Errorf("marshal symbols: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) handleSearchByPattern(ctx context.Context, input json.RawMessage) (string, error) {
	var args struct {
		FilePath string `json:"file_path"`
		Pattern  string `json:"pattern"`
		Source   string `json:"source"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	if args.Pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	source, lang, err := resolveSource(args.FilePath, args.Source, args.Language)
	if err != nil {
		return "", err
	}

	matches, err := SearchText(ctx, h.parser, source, lang, args.Pattern, args.FilePath)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(matches)
	if err != nil {
		return "", fmt.Errorf("marshal matches: %w", err)
	}
	return string(out), nil
}

func (h *MCPHandler) handleSupportedLanguages() (string, error) {
	out, err := json.Marshal(SupportedLanguages())
	if err != nil {
		return "", fmt.Errorf("marshal languages: %w", err)
	}
	return string(out), nil
}
