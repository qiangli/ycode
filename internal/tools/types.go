package tools

import (
	"context"
	"encoding/json"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// ToolSource identifies where a tool was registered from.
type ToolSource string

const (
	SourceBuiltin ToolSource = "builtin"
	SourcePlugin  ToolSource = "plugin"
	SourceMCP     ToolSource = "mcp"
)

// ToolCategory classifies a tool for concurrency scheduling.
type ToolCategory int

const (
	CategoryStandard    ToolCategory = iota // file ops, search, web — default
	CategoryLLM                             // Agent, TaskCreate — uses LLM API
	CategoryInteractive                     // AskUserQuestion — blocks on user input
)

// ToolFunc is the handler function signature for a tool.
type ToolFunc func(ctx context.Context, input json.RawMessage) (string, error)

// ToolSpec defines a tool's metadata and behavior.
type ToolSpec struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	InputSchema     json.RawMessage `json:"input_schema"`
	RequiredMode    permission.Mode `json:"-"`
	Source          ToolSource      `json:"-"`
	Handler         ToolFunc        `json:"-"`
	AlwaysAvailable bool            `json:"-"` // sent in every request vs deferred
	Category        ToolCategory    `json:"-"` // concurrency scheduling category
}

// RuntimeToolDefinition is the API-facing tool definition.
type RuntimeToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// ToAPIDefinition converts a ToolSpec to the API tool definition format.
func (ts *ToolSpec) ToAPIDefinition() RuntimeToolDefinition {
	return RuntimeToolDefinition{
		Name:        ts.Name,
		Description: ts.Description,
		InputSchema: ts.InputSchema,
	}
}

// Middleware wraps a ToolFunc to add cross-cutting concerns.
type Middleware func(ToolFunc) ToolFunc
