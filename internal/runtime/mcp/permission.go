package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// PermissionMode is the permission tier required to invoke a tool.
// Mirrors internal/tools.PermissionMode but kept independent so the mcp
// package has no dependency on the in-process tool registry.
type PermissionMode string

const (
	ModeReadOnly         PermissionMode = "ReadOnly"
	ModeWorkspaceWrite   PermissionMode = "WorkspaceWrite"
	ModeDangerFullAccess PermissionMode = "DangerFullAccess"
)

// rank assigns a numeric level to each mode so a gate can compare them.
// Higher rank = more privilege.
func (m PermissionMode) rank() int {
	switch m {
	case ModeReadOnly:
		return 0
	case ModeWorkspaceWrite:
		return 1
	case ModeDangerFullAccess:
		return 2
	default:
		return 0
	}
}

// PermissionAware is an optional interface a ServerHandler can implement to
// declare each tool's required permission mode. Handlers that do not
// implement it are treated as ReadOnly. New capability families that expose
// write-capable tools must implement this — the gate cannot guess.
type PermissionAware interface {
	RequiredMode(toolName string) PermissionMode
}

// PermissionGate decides whether a tool call is allowed.
//
// Implementations:
//   - StaticGate — allow only at or below a fixed ceiling. The right default
//     for standalone `ycode mcp serve` where there is no human-loop client
//     to prompt.
//   - (future) PromptingGate — route to a RemotePermissionPrompter for live
//     human approval. Wired in `ycode serve`.
type PermissionGate interface {
	Allow(ctx context.Context, toolName string, mode PermissionMode, input json.RawMessage) (bool, error)
}

// StaticGate allows tool calls at or below Ceiling. Anything above is denied
// without prompting.
type StaticGate struct {
	Ceiling PermissionMode
}

func (s StaticGate) Allow(_ context.Context, _ string, mode PermissionMode, _ json.RawMessage) (bool, error) {
	return mode.rank() <= s.Ceiling.rank(), nil
}

// GatedHandler wraps a ServerHandler with a permission gate. tools/call
// invocations consult the gate; tools/list and resources/* pass through
// unchanged so capability discovery stays cheap and unauthenticated.
type GatedHandler struct {
	Inner ServerHandler
	Gate  PermissionGate
}

// NewGatedHandler wraps inner with gate. If gate is nil, behaves as a
// pass-through (used in tests; production callers must always pass a gate).
func NewGatedHandler(inner ServerHandler, gate PermissionGate) *GatedHandler {
	return &GatedHandler{Inner: inner, Gate: gate}
}

func (g *GatedHandler) ListTools() []Tool         { return g.Inner.ListTools() }
func (g *GatedHandler) ListResources() []Resource { return g.Inner.ListResources() }

func (g *GatedHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	mode := ModeReadOnly
	if pa, ok := g.Inner.(PermissionAware); ok {
		mode = pa.RequiredMode(name)
	}
	if g.Gate != nil {
		allowed, err := g.Gate.Allow(ctx, name, mode, input)
		if err != nil {
			return "", fmt.Errorf("permission gate: %w", err)
		}
		if !allowed {
			return "", fmt.Errorf("permission denied for tool %q (required: %s)", name, mode)
		}
	}
	return g.Inner.HandleToolCall(ctx, name, input)
}

func (g *GatedHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	return g.Inner.ReadResource(ctx, uri)
}
