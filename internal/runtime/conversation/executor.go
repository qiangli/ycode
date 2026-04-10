package conversation

import (
	"context"
	"encoding/json"
)

// ToolExecutor is the interface for executing tools.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// SubagentToolExecutor wraps a tool registry for subagent use.
type SubagentToolExecutor struct {
	invoker func(ctx context.Context, name string, input json.RawMessage) (string, error)
}

// NewSubagentToolExecutor creates a new subagent tool executor.
func NewSubagentToolExecutor(invoker func(ctx context.Context, name string, input json.RawMessage) (string, error)) *SubagentToolExecutor {
	return &SubagentToolExecutor{invoker: invoker}
}

// Execute invokes a tool by name.
func (e *SubagentToolExecutor) Execute(ctx context.Context, name string, input json.RawMessage) (string, error) {
	return e.invoker(ctx, name, input)
}
