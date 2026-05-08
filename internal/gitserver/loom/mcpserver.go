package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/pkg/loom"
)

// MCPHandler exposes the loom substrate over JSON-RPC. It is a thin
// adapter that unmarshals tool arguments into pkg/loom request DTOs,
// dispatches to a Service, and renders the result as a single text
// block.
type MCPHandler struct {
	svc *loom.Service
}

// NewMCPHandler wraps a loom.Service in an mcp.ServerHandler.
func NewMCPHandler(svc *loom.Service) *MCPHandler {
	return &MCPHandler{svc: svc}
}

// Compile-time assertions: MCPHandler satisfies both interfaces.
var (
	_ mcp.ServerHandler   = (*MCPHandler)(nil)
	_ mcp.PermissionAware = (*MCPHandler)(nil)
)

// Tool names. Foreign tools call these; the substrate's mental model
// is "five verbs over an opaque loom_id handle".
const (
	ToolLease   = "loom_lease"
	ToolPush    = "loom_push"
	ToolMerge   = "loom_merge"
	ToolStatus  = "loom_status"
	ToolRelease = "loom_release"
)

func (h *MCPHandler) ListTools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        ToolLease,
			Description: "Reserve an isolated git workspace (clone+branch+author identity) for a sub-agent. Returns a loom_id handle plus the sandbox path the sub-agent should work in. The substrate guarantees N parallel leases for the same project never step on each other.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"cwd":              {"type": "string", "description": "Absolute path of the host project (foreign tool's caller cwd)."},
					"sub_agent_label":  {"type": "string", "description": "Short identifier for this sub-agent. Becomes part of the branch name and git author trailer."},
					"ttl_seconds":      {"type": "integer", "description": "Optional. Lease lifetime in seconds (default 3600, max 28800)."},
					"base_branch":      {"type": "string", "description": "Optional. Base branch to cut from (default main)."}
				},
				"required": ["cwd", "sub_agent_label"]
			}`),
		},
		{
			Name:        ToolPush,
			Description: "Stage and commit every change in the lease's sandbox, then push the branch upstream. Idempotent — if there are no changes, no commit is made (the existing HEAD is still pushed).",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id":  {"type": "string", "description": "Lease handle from loom_lease."},
					"message":  {"type": "string", "description": "Optional commit message. Defaults to 'loom: <sub_agent_label>'."},
					"force":    {"type": "boolean", "description": "Allow non-fast-forward push (e.g. after rebase)."}
				},
				"required": ["loom_id"]
			}`),
		},
		{
			Name:        ToolMerge,
			Description: "Open a PR from the lease's branch into main. The merger handles auto-merge once CI is green. Idempotent — if a PR is already open, returns its number.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id": {"type": "string", "description": "Lease handle from loom_lease."},
					"title":   {"type": "string", "description": "Optional PR title."},
					"body":    {"type": "string", "description": "Optional PR body."}
				},
				"required": ["loom_id"]
			}`),
		},
		{
			Name:        ToolStatus,
			Description: "Report the state of one or more leases. Pass loom_id for a specific lease, cwd for all leases in a project, or neither for everything. States: leased, pushed, merging, merged, ci_failed, conflict.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id": {"type": "string", "description": "Optional. Filter to a single lease."},
					"cwd":     {"type": "string", "description": "Optional. Filter to a project (host cwd)."}
				}
			}`),
		},
		{
			Name:        ToolRelease,
			Description: "Tear down a lease. Removes the sandbox and (by default) the branch — but only if no PR is still open. Open PRs are left for the merger to finish.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id":     {"type": "string", "description": "Lease handle to release."},
					"keep_branch": {"type": "boolean", "description": "If true, do not delete the remote branch even when no PR is open."}
				},
				"required": ["loom_id"]
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource {
	return nil
}

func (h *MCPHandler) ReadResource(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("loom: no resources")
}

// RequiredMode encodes the permission tier for each tool. Read-capable
// status passes a ReadOnly gate; the four mutating verbs require
// WorkspaceWrite. The stdio MCP server's StaticGate{Ceiling: ReadOnly}
// will deny writes there — that is by design. Foreign tools route
// mutating calls via HTTP MCP under `ycode serve`, where the gate can
// authorize writes via the prompting flow.
func (h *MCPHandler) RequiredMode(toolName string) mcp.PermissionMode {
	switch toolName {
	case ToolStatus:
		return mcp.ModeReadOnly
	case ToolLease, ToolPush, ToolMerge, ToolRelease:
		return mcp.ModeWorkspaceWrite
	default:
		return mcp.ModeReadOnly
	}
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	case ToolLease:
		return h.handleLease(ctx, input)
	case ToolPush:
		return h.handlePush(ctx, input)
	case ToolMerge:
		return h.handleMerge(ctx, input)
	case ToolStatus:
		return h.handleStatus(ctx, input)
	case ToolRelease:
		return h.handleRelease(ctx, input)
	default:
		return "", fmt.Errorf("loom: unknown tool %q", name)
	}
}

func (h *MCPHandler) handleLease(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.LeaseRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_lease: %w", err)
	}
	lease, err := h.svc.Lease(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalResult(lease)
}

func (h *MCPHandler) handlePush(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.PushRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_push: %w", err)
	}
	res, err := h.svc.Push(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalResult(res)
}

func (h *MCPHandler) handleMerge(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.MergeRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_merge: %w", err)
	}
	res, err := h.svc.Merge(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalResult(res)
}

func (h *MCPHandler) handleStatus(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.StatusRequest
	// Empty body means "all leases"; tolerate missing JSON.
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("loom_status: %w", err)
		}
	}
	statuses, err := h.svc.Status(ctx, req)
	if err != nil {
		// loom.ErrLeaseNotFound is a semantic empty result for status.
		if errors.Is(err, loom.ErrLeaseNotFound) {
			return marshalResult([]loom.LeaseStatus{})
		}
		return "", err
	}
	if statuses == nil {
		statuses = []loom.LeaseStatus{}
	}
	return marshalResult(statuses)
}

func (h *MCPHandler) handleRelease(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.ReleaseRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_release: %w", err)
	}
	if err := h.svc.Release(ctx, req); err != nil {
		return "", err
	}
	return marshalResult(map[string]any{"released": true, "loom_id": req.LoomID})
}

func marshalResult(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mustJSON parses a JSON string into a json.RawMessage at init time.
// Panics if the literal is malformed — caught immediately in tests.
func mustJSON(s string) json.RawMessage {
	var raw json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		panic(fmt.Sprintf("loom: invalid tool schema literal: %v\n%s", err, strings.TrimSpace(s)))
	}
	return raw
}
