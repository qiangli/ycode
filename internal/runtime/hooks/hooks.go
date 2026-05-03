// Package hooks provides a general-purpose hook system for intercepting
// tool execution, session events, and file changes. Hooks are user-configurable
// via settings.json and execute shell commands with a JSON protocol.
//
// The Runner type provides simple event→command execution.
// The Registry type adds pattern matching, priority ordering, and response parsing.
package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Standard hook event names.
const (
	EventPreToolUse      = "PreToolUse"
	EventPostToolUse     = "PostToolUse"
	EventPostToolFailure = "PostToolUseFailure"
	EventSessionStart    = "SessionStart"
	EventSessionEnd      = "SessionEnd"
	EventFileChanged     = "FileChanged"
	EventTurnStart       = "TurnStart"
)

// HookAction is the hook's decision.
type HookAction string

const (
	ActionContinue HookAction = "continue"
	ActionBlock    HookAction = "block"
)

// HookResponse controls what happens after a hook runs.
type HookResponse struct {
	Action       HookAction      `json:"action"`
	Message      string          `json:"message,omitempty"`       // inject as system message
	UpdatedInput json.RawMessage `json:"updated_input,omitempty"` // modify tool args (PreToolUse only)
	Decision     string          `json:"decision,omitempty"`      // "approve" / "block" for permissions
}

// HookHandler executes a single hook with response parsing.
type HookHandler interface {
	Execute(ctx context.Context, event string, payload *Event) (*HookResponse, error)
}

// Registration binds a handler to an event with metadata.
type Registration struct {
	Handler  HookHandler
	Match    string // tool name pattern (empty = match all)
	Priority int
}

// Registry dispatches hook events to registered handlers with pattern matching
// and priority ordering. It extends the basic Runner with response parsing.
type Registry struct {
	handlers map[string][]Registration
	mu       sync.RWMutex
}

// NewRegistry creates an empty hook registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string][]Registration),
	}
}

// Register adds a handler for a hook event.
func (r *Registry) Register(event string, reg Registration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[event] = append(r.handlers[event], reg)
}

// Run executes all handlers for an event and returns the first blocking response.
// If no handler blocks, returns nil (continue).
func (r *Registry) Run(ctx context.Context, event string, payload *Event) (*HookResponse, error) {
	r.mu.RLock()
	regs := r.handlers[event]
	r.mu.RUnlock()

	if len(regs) == 0 {
		return nil, nil
	}

	for _, reg := range regs {
		// Check tool name pattern match for tool-specific hooks.
		if reg.Match != "" && payload.ToolName != "" {
			if !matchHookPattern(reg.Match, payload.ToolName) {
				continue
			}
		}

		resp, err := reg.Handler.Execute(ctx, event, payload)
		if err != nil {
			return nil, fmt.Errorf("hook %s: %w", event, err)
		}
		if resp != nil && resp.Action == ActionBlock {
			return resp, nil
		}
	}

	return nil, nil
}

// HasHandlers returns true if any handlers are registered for the event.
func (r *Registry) HasHandlers(event string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[event]) > 0
}

// matchHookPattern checks if a tool name matches a pattern.
func matchHookPattern(pattern, tool string) bool {
	if pattern == "*" || pattern == tool {
		return true
	}
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(tool) >= len(prefix) && tool[:len(prefix)] == prefix
	}
	return false
}
