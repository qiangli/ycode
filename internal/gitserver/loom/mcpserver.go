package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// Tool names. v1 ("five verbs over an opaque loom_id handle") and v2
// (sub-agent + orchestrator role triplets) coexist during the N+0
// migration window. v1 verbs are deprecated; agents prefer the v2 set.
const (
	// v1 (deprecated; removed in N+2).
	ToolLease   = "loom_lease"
	ToolPush    = "loom_push"
	ToolMerge   = "loom_merge"
	ToolStatus  = "loom_status"
	ToolRelease = "loom_release"

	// v2 sub-agent role — active inside a workspace.
	ToolCheckpoint = "loom_checkpoint"
	ToolSubmit     = "loom_submit"
	ToolAbandon    = "loom_abandon"

	// v2 orchestrator role — active outside any workspace.
	ToolOpen      = "loom_open"
	ToolTerminate = "loom_terminate"
	ToolHandoff   = "loom_handoff"

	// v2 resources.
	ResourceSession = "loom://session"
	ResourceProject = "loom://project"
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

		// v2 sub-agent role.
		{
			Name:        ToolCheckpoint,
			Description: "Lightweight save point inside the current lease's sandbox. Stages every change and makes a local commit (no push). Idempotent: empty staging area returns the current HEAD SHA. Loom_id is read from YCODE_LOOM_ID when omitted.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id": {"type": "string", "description": "Lease handle. Omit to use YCODE_LOOM_ID."},
					"summary": {"type": "string", "description": "Optional commit message. Defaults to 'loom: checkpoint (<sub_agent_label>)'."}
				}
			}`),
		},
		{
			Name:        ToolSubmit,
			Description: "Push branch, open or refresh the PR against main, and block until terminal state (merged | conflict | ci_failed) or max_wait_seconds elapses (returns pending). On conflict the sandbox is rebased in place with conflict markers preserved so the agent can edit and resubmit.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id":          {"type": "string", "description": "Lease handle. Omit to use YCODE_LOOM_ID."},
					"title":            {"type": "string", "description": "Optional PR title."},
					"body":             {"type": "string", "description": "Optional PR body."},
					"message":          {"type": "string", "description": "Optional commit message."},
					"force":            {"type": "boolean", "description": "Allow non-fast-forward push."},
					"max_wait_seconds": {"type": "integer", "description": "How long to block waiting for terminal state (default 300)."}
				}
			}`),
		},
		{
			Name:        ToolAbandon,
			Description: "Tear down the current sandbox. Equivalent to loom_release, scoped to the sub-agent role. Branch retained if a PR is open.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id": {"type": "string", "description": "Lease handle. Omit to use YCODE_LOOM_ID."},
					"reason":  {"type": "string", "description": "Optional human-readable reason for logs."}
				}
			}`),
		},

		// v2 orchestrator role.
		{
			Name:        ToolOpen,
			Description: "Allocate a workspace for a sub-agent. Returns loom_id, sandbox path, branch, and author identity. Parent process spawns the sub-agent into this workspace.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"cwd":             {"type": "string", "description": "Absolute path of the host project (parent's caller cwd)."},
					"sub_agent_label": {"type": "string", "description": "Short identifier; becomes part of branch name + author trailer."},
					"ttl_seconds":     {"type": "integer", "description": "Optional. Lease lifetime (default 3600, max 28800)."},
					"base_branch":     {"type": "string", "description": "Optional. Base branch (default main)."}
				},
				"required": ["cwd", "sub_agent_label"]
			}`),
		},
		{
			Name:        ToolTerminate,
			Description: "Forcibly tear down a sub-agent's lease. Same sandbox + branch cleanup as loom_release; intended for the parent's monitoring path.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"loom_id":     {"type": "string", "description": "Lease to terminate."},
					"keep_branch": {"type": "boolean", "description": "If true, retain the remote branch."}
				},
				"required": ["loom_id"]
			}`),
		},
		{
			Name:        ToolHandoff,
			Description: "Allocate a workspace AND return the env vars + sandbox path the parent should use when spawning the sub-agent process. Like loom_open but with sub-agent-friendly env shape pre-computed. The parent is responsible for the actual exec.",
			InputSchema: mustJSON(`{
				"type": "object",
				"properties": {
					"cwd":             {"type": "string", "description": "Absolute path of the host project."},
					"sub_agent_label": {"type": "string", "description": "Short identifier."},
					"ttl_seconds":     {"type": "integer", "description": "Optional. Lease lifetime (default 3600, max 28800)."},
					"base_branch":     {"type": "string", "description": "Optional. Base branch (default main)."}
				},
				"required": ["cwd", "sub_agent_label"]
			}`),
		},
	}
}

func (h *MCPHandler) ListResources() []mcp.Resource {
	return []mcp.Resource{
		{
			URI:         ResourceSession,
			Name:        "loom session",
			Description: "Current state of the lease named by YCODE_LOOM_ID (or query string ?loom_id=...). Returns a JSON snapshot — clients poll for transitions. Real-time SSE streaming lands in a follow-up.",
			MimeType:    "application/json",
		},
		{
			URI:         ResourceProject,
			Name:        "loom project",
			Description: "Snapshot of every active lease scoped by ?cwd=... query string (or all leases if absent). Returns JSON. Used by `ycode weave list` and the orchestrator's monitoring path.",
			MimeType:    "application/json",
		},
	}
}

func (h *MCPHandler) ReadResource(ctx context.Context, uri string) (string, error) {
	// Strip an optional query string for filter parameters; today the
	// snapshot reads pkg/loom.Service.Status with the appropriate
	// filter and renders JSON. Streaming SSE arrives in a later PR.
	base, _, _ := strings.Cut(uri, "?")
	switch base {
	case ResourceSession:
		statuses, err := h.svc.Status(ctx, loom.StatusRequest{
			LoomID: os.Getenv("YCODE_LOOM_ID"),
		})
		if err != nil {
			return "", err
		}
		return marshalResult(statuses)
	case ResourceProject:
		statuses, err := h.svc.Status(ctx, loom.StatusRequest{})
		if err != nil {
			return "", err
		}
		return marshalResult(statuses)
	default:
		return "", fmt.Errorf("loom: unknown resource %q", uri)
	}
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
	case ToolLease, ToolPush, ToolMerge, ToolRelease,
		ToolCheckpoint, ToolSubmit, ToolAbandon,
		ToolOpen, ToolTerminate, ToolHandoff:
		return mcp.ModeWorkspaceWrite
	default:
		return mcp.ModeReadOnly
	}
}

func (h *MCPHandler) HandleToolCall(ctx context.Context, name string, input json.RawMessage) (string, error) {
	switch name {
	// v1.
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

	// v2 sub-agent role.
	case ToolCheckpoint:
		return h.handleCheckpoint(ctx, input)
	case ToolSubmit:
		return h.handleSubmit(ctx, input)
	case ToolAbandon:
		return h.handleAbandon(ctx, input)

	// v2 orchestrator role.
	case ToolOpen:
		return h.handleOpen(ctx, input)
	case ToolTerminate:
		return h.handleTerminate(ctx, input)
	case ToolHandoff:
		return h.handleHandoff(ctx, input)

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

// resolveLoomID picks the loom_id from the request body if present, or
// from YCODE_LOOM_ID. Sub-agent verbs accept either so the auto-attach
// env in N0.7 carries the handle implicitly.
func resolveLoomID(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return os.Getenv("YCODE_LOOM_ID")
}

func (h *MCPHandler) handleCheckpoint(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.CheckpointRequest
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("loom_checkpoint: %w", err)
		}
	}
	req.LoomID = resolveLoomID(req.LoomID)
	if req.LoomID == "" {
		return "", fmt.Errorf("loom_checkpoint: missing loom_id (set YCODE_LOOM_ID or pass explicitly)")
	}
	res, err := h.svc.Checkpoint(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalResult(res)
}

func (h *MCPHandler) handleSubmit(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.SubmitRequest
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("loom_submit: %w", err)
		}
	}
	req.LoomID = resolveLoomID(req.LoomID)
	if req.LoomID == "" {
		return "", fmt.Errorf("loom_submit: missing loom_id")
	}
	res, err := h.svc.SubmitAndWait(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalResult(res)
}

func (h *MCPHandler) handleAbandon(ctx context.Context, input json.RawMessage) (string, error) {
	// Body shape: {loom_id?, reason?, keep_branch?}. reason is currently
	// log-only metadata.
	var req struct {
		LoomID     string `json:"loom_id"`
		Reason     string `json:"reason,omitempty"`
		KeepBranch bool   `json:"keep_branch,omitempty"`
	}
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &req); err != nil {
			return "", fmt.Errorf("loom_abandon: %w", err)
		}
	}
	req.LoomID = resolveLoomID(req.LoomID)
	if req.LoomID == "" {
		return "", fmt.Errorf("loom_abandon: missing loom_id")
	}
	if err := h.svc.Release(ctx, loom.ReleaseRequest{LoomID: req.LoomID, KeepBranch: req.KeepBranch}); err != nil {
		return "", err
	}
	return marshalResult(map[string]any{"abandoned": true, "loom_id": req.LoomID})
}

func (h *MCPHandler) handleOpen(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.LeaseRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_open: %w", err)
	}
	lease, err := h.svc.Lease(ctx, req)
	if err != nil {
		return "", err
	}
	return marshalResult(lease)
}

func (h *MCPHandler) handleTerminate(ctx context.Context, input json.RawMessage) (string, error) {
	var req loom.ReleaseRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_terminate: %w", err)
	}
	if err := h.svc.Release(ctx, req); err != nil {
		return "", err
	}
	return marshalResult(map[string]any{"terminated": true, "loom_id": req.LoomID})
}

func (h *MCPHandler) handleHandoff(ctx context.Context, input json.RawMessage) (string, error) {
	// Allocate via Lease, then return the env shape the parent needs to
	// exec a sub-agent. The actual exec is the parent's responsibility;
	// the substrate only owns the workspace.
	var req loom.LeaseRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return "", fmt.Errorf("loom_handoff: %w", err)
	}
	lease, err := h.svc.Lease(ctx, req)
	if err != nil {
		return "", err
	}
	out := map[string]any{
		"loom_id":      lease.ID,
		"sandbox_path": lease.Path,
		"branch":       lease.Branch,
		"env": map[string]string{
			"YCODE_LOOM_ID":     lease.ID,
			"YCODE_LOOM_BRANCH": lease.Branch,
			"YCODE_LOOM_BASE":   loom.DefaultBaseBranch,
			"YCODE_LOOM_LABEL":  lease.SubAgentLabel,
		},
	}
	return marshalResult(out)
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
