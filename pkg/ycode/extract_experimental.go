//go:build experimental

package ycode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/api"
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
	if maxTokens == 0 {
		maxTokens = 4096
	}

	rf := &api.ResponseFormat{
		Type:   "json_schema",
		Name:   opts.SchemaName,
		Schema: opts.Schema,
	}
	if len(opts.Schema) == 0 {
		rf.Type = "json_object"
	}
	if rf.Name == "" {
		rf.Name = "extract"
	}

	req := &api.Request{
		Model:     model,
		MaxTokens: maxTokens,
		System:    opts.System,
		Messages: []api.Message{{
			Role:    api.RoleUser,
			Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: prompt}},
		}},
		Stream:         false,
		ResponseFormat: rf,
	}

	events, errc := a.provider.Send(ctx, req)

	var text strings.Builder     // OpenAI-compat: json_schema arrives as text content
	var toolJSON strings.Builder // Anthropic shim: forced tool_use input arrives as input_json_delta
	for ev := range events {
		if ev.Type != "content_block_delta" || len(ev.Delta) == 0 {
			continue
		}
		var d struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		}
		if err := json.Unmarshal(ev.Delta, &d); err != nil {
			continue
		}
		switch d.Type {
		case "text_delta":
			text.WriteString(d.Text)
		case "input_json_delta":
			toolJSON.WriteString(d.PartialJSON)
		}
	}
	if err := <-errc; err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	out := toolJSON.String()
	if out == "" {
		out = text.String()
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, fmt.Errorf("extract: provider returned no JSON content")
	}
	return []byte(out), nil
}
