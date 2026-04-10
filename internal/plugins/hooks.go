package plugins

import (
	"context"
	"encoding/json"
)

// HookEvent identifies when a hook fires.
type HookEvent string

const (
	HookPreToolUse         HookEvent = "pre_tool_use"
	HookPostToolUse        HookEvent = "post_tool_use"
	HookPostToolUseFailure HookEvent = "post_tool_use_failure"
)

// HookPayload is the data passed to a hook.
type HookPayload struct {
	Event    HookEvent       `json:"event"`
	ToolName string          `json:"tool_name"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   string          `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// Hook is a function that runs at a specific point in tool execution.
type Hook func(ctx context.Context, payload *HookPayload) error

// HookRegistry holds hooks from plugins.
type HookRegistry struct {
	hooks map[HookEvent][]Hook
}

// NewHookRegistry creates a new hook registry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		hooks: make(map[HookEvent][]Hook),
	}
}

// Register adds a hook for an event.
func (hr *HookRegistry) Register(event HookEvent, hook Hook) {
	hr.hooks[event] = append(hr.hooks[event], hook)
}

// Fire executes all hooks for an event.
func (hr *HookRegistry) Fire(ctx context.Context, payload *HookPayload) error {
	hooks := hr.hooks[payload.Event]
	for _, hook := range hooks {
		if err := hook(ctx, payload); err != nil {
			return err
		}
	}
	return nil
}
