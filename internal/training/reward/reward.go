package reward

import "context"

// AgentResult holds the outcome of an agent rollout for reward scoring.
type AgentResult struct {
	Messages   []Message   // full conversation history
	TurnsUsed  int         // number of turns used
	ToolErrors []ToolError // tool execution errors
	Finished   bool        // true if agent stopped naturally
}

// Message is a simplified conversation message for reward computation.
type Message struct {
	Role      string     // "system", "user", "assistant", "tool"
	Content   string     // message content
	ToolName  string     // for tool messages
	ToolCalls []ToolCall // tool invocations
}

// ToolCall represents a tool invocation by the assistant.
type ToolCall struct {
	Name      string // tool name
	Arguments string // raw JSON arguments
}

// ToolError records a tool execution error.
type ToolError struct {
	Turn     int    // turn number
	ToolName string // tool that errored
	Error    string // error message
}

// RewardFunc scores an agent's performance on a task.
type RewardFunc interface {
	// Score returns a reward between 0.0 and 1.0.
	Score(ctx context.Context, result *AgentResult) (float64, error)
}
