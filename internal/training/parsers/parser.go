package parsers

// ToolCall represents a parsed tool invocation.
type ToolCall struct {
	ID        string // call identifier
	Name      string // tool name
	Arguments string // raw JSON arguments
}

// Parser extracts tool calls from raw model output.
type Parser interface {
	// Name returns the parser identifier (e.g., "hermes", "json").
	Name() string

	// Parse extracts text content and tool calls from raw output.
	Parse(rawOutput string) (content string, toolCalls []ToolCall, err error)
}
