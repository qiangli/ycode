package ycode

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/extract"
)

// ExtractOptions configures (*Agent).Extract.
type ExtractOptions struct {
	// Model overrides the Agent's configured chat model for this call. Empty
	// means use the Agent's default.
	Model string

	// MaxTokens caps the response. Zero means inherit the Agent's default.
	MaxTokens int

	// System is an optional system prompt added before the user message.
	System string

	// Schema is a JSON Schema document describing the expected response. When
	// non-empty, Extract uses Type "json_schema". When empty, Extract falls
	// back to Type "json_object" — the model emits any JSON object.
	Schema json.RawMessage

	// SchemaName labels the schema for providers that require it (OpenAI's
	// response_format.json_schema.name). Defaults to "extract".
	SchemaName string
}

// Provider returns the underlying api.Provider configured on this Agent.
//
// This is the escape hatch for callers that need direct access to the LLM
// (custom streaming UIs, non-Extract structured-output patterns). Most hosts
// should prefer Extract, which handles model selection, wire-format
// translation, and Anthropic's forced-tool_use shim.
func (a *Agent) Provider() api.Provider {
	return a.provider
}

// Extract runs a single non-agentic LLM call constrained to emit JSON output.
//
// For OpenAI-compatible providers the schema is sent as response_format.
// For Anthropic the schema is translated into a forced tool_use call (handled
// transparently by applyResponseFormatShim inside the api package).
//
// Returns the raw JSON bytes. Callers unmarshal into their own typed struct.
// No agent loop, no tools, no memory — one round-trip to the provider.
func (a *Agent) Extract(ctx context.Context, prompt string, opts ExtractOptions) ([]byte, error) {
	if a.provider == nil {
		return nil, fmt.Errorf("extract: agent has no provider configured")
	}
	model := opts.Model
	if model == "" {
		model = a.config.Model
	}
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = a.config.MaxTokens
	}
	return extract.Run(ctx, a.provider, extract.Options{
		Model:      model,
		MaxTokens:  maxTokens,
		System:     opts.System,
		Schema:     opts.Schema,
		SchemaName: opts.SchemaName,
	}, prompt)
}
