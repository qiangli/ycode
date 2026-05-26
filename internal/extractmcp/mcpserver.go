// Package extractmcp exposes ycode's one-shot extraction primitives
// as MCP tools so foreign agents can read documents and request
// structured-JSON LLM extractions without the HTTP /api/extract
// detour.
//
// Two handlers:
//
//   - NewDocumentHandler() — stateless. Reads PDF/DOCX/XLSX/PPTX/CSV
//     into text via internal/tools.ReadDocument. No provider, no I/O
//     beyond the filesystem. Safe in stdio and HTTP composites.
//
//   - NewJSONHandler(provider, ...) — provider-backed. One LLM round
//     trip with optional JSON schema, returns raw JSON bytes. Wraps
//     internal/runtime/extract.Run. ONLY registered in the HTTP
//     composite — the stdio composite doesn't construct a provider.
//
// Both handlers declare ReadOnly permission: document reads don't
// mutate the workspace, and an LLM extraction has no side effects
// beyond provider-side billing.
package extractmcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/extract"
	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/internal/tools"
)

// DocumentHandler exposes the `extract_document` tool.
type DocumentHandler struct{}

// NewDocumentHandler returns a stateless handler. No deps, no init I/O.
func NewDocumentHandler() *DocumentHandler { return &DocumentHandler{} }

func (h *DocumentHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{{
		Name: "extract_document",
		Description: "Extract text from a PDF, DOCX, XLSX, PPTX, or CSV file. " +
			"Returns the document's textual content as a plain string. " +
			"`pages` is optional for PDFs (formats: \"1-5\", \"3\", \"1,3,5\"); " +
			"ignored for other types.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"file_path": {"type": "string", "description": "Absolute path to the document."},
				"pages": {"type": "string", "description": "PDF page range, e.g. \"1-5\". Optional."}
			},
			"required": ["file_path"]
		}`),
	}}
}

func (h *DocumentHandler) ListResources() []mcp.Resource { return nil }

func (h *DocumentHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("extract_document: no resources exposed")
}

func (h *DocumentHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *DocumentHandler) HandleToolCall(_ context.Context, name string, input json.RawMessage) (string, error) {
	if name != "extract_document" {
		return "", fmt.Errorf("unknown tool: %q", name)
	}
	var args struct {
		FilePath string `json:"file_path"`
		Pages    string `json:"pages"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.FilePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	return tools.ReadDocument(args.FilePath, args.Pages)
}

// ============================================================================
// JSONHandler — provider-backed LLM extraction (HTTP-only)
// ============================================================================

// JSONHandler exposes the `extract_json` tool. Wraps a single LLM
// round-trip with optional JSON schema, returning raw JSON bytes.
//
// IMPORTANT: This handler MUST only be registered when a provider is
// available. The stdio `ycode mcp serve` path does not construct a
// provider — registering JSONHandler there would advertise a tool that
// always fails. See cmd/ycode/serve.go for the canonical registration.
type JSONHandler struct {
	provider  api.Provider
	model     string // default model; "" defers to provider default
	maxTokens int    // default cap; 0 defers to extract.Run's fallback
}

// NewJSONHandler builds the handler. provider must be non-nil — the
// constructor returns nil if it isn't, so the caller's append-if-non-nil
// pattern is the natural guard.
func NewJSONHandler(provider api.Provider, defaultModel string, defaultMaxTokens int) *JSONHandler {
	if provider == nil {
		return nil
	}
	return &JSONHandler{provider: provider, model: defaultModel, maxTokens: defaultMaxTokens}
}

func (h *JSONHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{{
		Name: "extract_json",
		Description: "Run a one-shot LLM extraction returning raw JSON. Stateless: no " +
			"session, no agent loop, no tools. With `schema` (JSON Schema), the " +
			"response conforms; without, the model returns any JSON object. " +
			"Mirrors HTTP POST /ycode/api/extract.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"prompt":      {"type": "string", "description": "User prompt describing what to extract."},
				"system":      {"type": "string", "description": "Optional system prompt."},
				"schema":      {"type": "object", "description": "Optional JSON Schema."},
				"schema_name": {"type": "string", "description": "Label for schema-aware providers. Defaults to \"extract\"."},
				"model":       {"type": "string", "description": "Override model. Empty = handler default."},
				"max_tokens":  {"type": "integer", "description": "Override max tokens. 0 = handler default."}
			},
			"required": ["prompt"]
		}`),
	}}
}

func (h *JSONHandler) ListResources() []mcp.Resource { return nil }

func (h *JSONHandler) ReadResource(_ context.Context, uri string) (string, error) {
	return "", fmt.Errorf("extract_json: no resources exposed")
}

func (h *JSONHandler) RequiredMode(_ string) mcp.PermissionMode {
	return mcp.ModeReadOnly
}

func (h *JSONHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if name != "extract_json" {
		return "", fmt.Errorf("unknown tool: %q", name)
	}
	var args struct {
		Prompt     string          `json:"prompt"`
		System     string          `json:"system"`
		Schema     json.RawMessage `json:"schema"`
		SchemaName string          `json:"schema_name"`
		Model      string          `json:"model"`
		MaxTokens  int             `json:"max_tokens"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}
	if args.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	model := args.Model
	if model == "" {
		model = h.model
	}
	maxTokens := args.MaxTokens
	if maxTokens == 0 {
		maxTokens = h.maxTokens
	}

	out, err := extract.Run(ctx, h.provider, extract.Options{
		Model:      model,
		MaxTokens:  maxTokens,
		System:     args.System,
		Schema:     args.Schema,
		SchemaName: args.SchemaName,
	}, args.Prompt)
	if err != nil {
		return "", err
	}
	return string(out), nil
}
