package repomap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/qiangli/ycode/internal/runtime/mcp"
)

// MCPHandler exposes ycode's repomap generator to external coding
// agents over MCP. Read-only: walks the filesystem under the given
// root and parses source files (Go via go/ast, others via in-process
// tree-sitter). No CGO, no external services.
//
// Foreign agents typically call this once early in a session to seed
// system-prompt context, then again only when the codebase shape
// changes materially.
type MCPHandler struct{}

// NewMCPHandler constructs the handler. Stateless — Generate builds
// a fresh map each call.
func NewMCPHandler() *MCPHandler { return &MCPHandler{} }

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name: "build_repomap",
			Description: "Generate a token-budgeted file→symbol overview of a repository. " +
				"Walks the tree rooted at `root` (defaults to the server's cwd), extracts top-level " +
				"declarations (types, functions, methods, classes) via go/ast for Go and in-process " +
				"tree-sitter for Python / JS / TS / Rust / Java / C / Ruby, ranks files by relevance " +
				"if `query` is given, and truncates to fit the token budget (~4 chars/token). " +
				"Returns the formatted human-readable map by default; pass `format=\"json\"` to get " +
				"the structured RepoMap as JSON. `budget` is accepted as an alias for `max_tokens`.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"root":       {"type": "string", "description": "Absolute or relative path to the repo root. Defaults to the server's cwd."},
					"max_tokens": {"type": "integer", "description": "Token budget. Default 4096 (~16KB). Set to 0 for no limit."},
					"budget":     {"type": "integer", "description": "Alias for max_tokens. If both are set, max_tokens wins."},
					"max_files":  {"type": "integer", "description": "Cap on file entries. 0 = use token budget instead."},
					"query":      {"type": "string", "description": "Optional. Rank files by relevance to this natural-language query."},
					"format":     {"type": "string", "enum": ["text", "json"], "description": "Output shape. Default text (human-readable Format())."}
				}
			}`),
		},
		{
			Name: "repomap_for_files",
			Description: "Like build_repomap, but parses an explicit list of files instead of walking " +
				"a tree. Useful when the agent already knows which files matter (open editor buffers, " +
				"PR-changed files, results of a prior search). Paths may be absolute or relative to " +
				"`root`. Unsupported extensions are silently skipped.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"files":      {"type": "array", "items": {"type": "string"}, "description": "Files to parse. Absolute, or relative to root."},
					"root":       {"type": "string", "description": "Base for resolving relative files. Defaults to the server cwd."},
					"max_tokens": {"type": "integer", "description": "Token budget. Default 4096."},
					"budget":     {"type": "integer", "description": "Alias for max_tokens. If both are set, max_tokens wins."},
					"max_files":  {"type": "integer", "description": "Cap on file entries. 0 = no cap."},
					"query":      {"type": "string", "description": "Optional. Rank files by relevance to this query."},
					"format":     {"type": "string", "enum": ["text", "json"], "description": "Output shape. Default text."}
				},
				"required": ["files"]
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode: both tools are pure reads (filesystem walk + parse).
// No writes, no shell, no network.
func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case "build_repomap":
		return h.handleBuild(input)
	case "repomap_for_files":
		return h.handleForFiles(input)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (h *MCPHandler) handleBuild(input json.RawMessage) (string, error) {
	var args struct {
		Root      string `json:"root"`
		MaxTokens int    `json:"max_tokens"`
		Budget    int    `json:"budget"`
		MaxFiles  int    `json:"max_files"`
		Query     string `json:"query"`
		Format    string `json:"format"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
	}
	root, err := resolveRoot(args.Root)
	if err != nil {
		return "", err
	}
	opts := buildOptions(args.MaxTokens, args.Budget, args.MaxFiles, args.Query)
	rm, err := Generate(root, opts)
	if err != nil {
		return "", fmt.Errorf("generate repomap: %w", err)
	}
	return formatRepoMap(rm, args.Format)
}

func (h *MCPHandler) handleForFiles(input json.RawMessage) (string, error) {
	var args struct {
		Files     []string `json:"files"`
		Root      string   `json:"root"`
		MaxTokens int      `json:"max_tokens"`
		Budget    int      `json:"budget"`
		MaxFiles  int      `json:"max_files"`
		Query     string   `json:"query"`
		Format    string   `json:"format"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
	}
	if len(args.Files) == 0 {
		return "", fmt.Errorf("files is required")
	}
	root, err := resolveRoot(args.Root)
	if err != nil {
		return "", err
	}
	opts := buildOptions(args.MaxTokens, args.Budget, args.MaxFiles, args.Query)
	rm, err := GenerateForFiles(root, args.Files, opts)
	if err != nil {
		return "", fmt.Errorf("generate repomap: %w", err)
	}
	return formatRepoMap(rm, args.Format)
}

func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	return cwd, nil
}

func buildOptions(maxTokens, budget, maxFiles int, query string) *Options {
	opts := DefaultOptions()
	switch {
	case maxTokens != 0:
		opts.MaxTokens = maxTokens
	case budget != 0:
		opts.MaxTokens = budget
	}
	if maxFiles > 0 {
		opts.MaxFiles = maxFiles
	}
	if query != "" {
		opts.RelevanceQuery = query
	}
	return opts
}

func formatRepoMap(rm *RepoMap, format string) (string, error) {
	if format == "json" {
		out, err := json.Marshal(rm)
		if err != nil {
			return "", fmt.Errorf("marshal repomap: %w", err)
		}
		return string(out), nil
	}
	return rm.Format(), nil
}

func (h *MCPHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("no resources: %s", uri)
}
