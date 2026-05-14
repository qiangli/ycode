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
				"if `query` is given, and truncates to fit `max_tokens` (~4 chars/token). " +
				"Returns the formatted human-readable map by default; pass `format=\"json\"` to get " +
				"the structured RepoMap as JSON.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"root":       {"type": "string", "description": "Absolute or relative path to the repo root. Defaults to the server's cwd."},
					"max_tokens": {"type": "integer", "description": "Token budget. Default 4096 (~16KB). Set to 0 for no limit."},
					"max_files":  {"type": "integer", "description": "Cap on file entries. 0 = use token budget instead."},
					"query":      {"type": "string", "description": "Optional. Rank files by relevance to this natural-language query."},
					"format":     {"type": "string", "enum": ["text", "json"], "description": "Output shape. Default text (human-readable Format())."}
				}
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource { return nil }

// RequiredMode: build_repomap is a pure read (filesystem walk + parse).
// No writes, no shell, no network.
func (h *MCPHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if name != "build_repomap" {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	var args struct {
		Root      string `json:"root"`
		MaxTokens int    `json:"max_tokens"`
		MaxFiles  int    `json:"max_files"`
		Query     string `json:"query"`
		Format    string `json:"format"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
	}

	root := args.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		root = cwd
	}

	opts := DefaultOptions()
	if args.MaxTokens != 0 {
		opts.MaxTokens = args.MaxTokens
	}
	if args.MaxFiles > 0 {
		opts.MaxFiles = args.MaxFiles
	}
	if args.Query != "" {
		opts.RelevanceQuery = args.Query
	}

	rm, err := Generate(root, opts)
	if err != nil {
		return "", fmt.Errorf("generate repomap: %w", err)
	}

	if args.Format == "json" {
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
