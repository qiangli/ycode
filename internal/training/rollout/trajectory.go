package rollout

import "time"

// ScoredTrajectory holds a complete agent rollout with reward scoring.
type ScoredTrajectory struct {
	ID         string        `json:"id"`
	TaskName   string        `json:"task_name"`
	ExampleID  string        `json:"example_id"`
	Messages   []Message     `json:"messages"`
	Score      float64       `json:"score"`
	TurnsUsed  int           `json:"turns_used"`
	ToolErrors int           `json:"tool_errors"`
	Duration   time.Duration `json:"duration_ns"`
	Finished   bool          `json:"finished_naturally"`
}

// Message is a conversation message in a trajectory.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall records a tool invocation.
type ToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}
