package loom

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/mcp"
	"github.com/qiangli/ycode/pkg/loom"
)

// stubBackend is a minimal pkg/loom.Backend impl just for handler-level
// tests. The exhaustive service behavior is covered by pkg/loom's own
// service_test.go; here we verify JSON-RPC shape, RequiredMode, and
// error propagation.
type stubBackend struct {
	mu sync.Mutex

	prs     map[string]int64 // slug:branch -> pr#
	prState map[string]string

	prCounter int64
}

func newStubBackend() *stubBackend {
	return &stubBackend{
		prs:     map[string]int64{},
		prState: map[string]string{},
	}
}

func (b *stubBackend) EnsureProject(_ context.Context, cwd string) (string, string, error) {
	return "slug-of-" + cwd, "http://stub/" + cwd + ".git", nil
}

func (b *stubBackend) PrepareSandbox(_ context.Context, root, slug, branch, agentID, name, email, cloneURL string) (string, error) {
	return root + "/" + agentID, nil
}

func (b *stubBackend) CommitAndPush(_ context.Context, path, slug, branch, message string, force bool) (string, error) {
	return "sha-" + branch, nil
}

func (b *stubBackend) EnsureRemoteBranch(_ context.Context, slug, branch string) error {
	return nil
}

func (b *stubBackend) OpenPR(_ context.Context, slug, branch, title, body string) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prCounter++
	n := b.prCounter
	b.prs[slug+":"+branch] = n
	b.prState[slug+":"+branch] = "open"
	return n, nil
}

func (b *stubBackend) ListPRStates(_ context.Context, slug, branchPrefix string) ([]loom.BackendPRState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []loom.BackendPRState
	for key, n := range b.prs {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || parts[0] != slug || !strings.HasPrefix(parts[1], branchPrefix) {
			continue
		}
		out = append(out, loom.BackendPRState{
			PRNumber: n,
			HeadRef:  parts[1],
			State:    b.prState[key],
		})
	}
	return out, nil
}

func (b *stubBackend) DeleteSandbox(_ string) error                             { return nil }
func (b *stubBackend) DeleteBranch(_ context.Context, _, _ string) error        { return nil }
func (b *stubBackend) NotifyProjectActive(_ context.Context, _, _ string) error { return nil }

func newTestHandler(t *testing.T) (*MCPHandler, *stubBackend) {
	t.Helper()
	backend := newStubBackend()
	svc, err := loom.NewService(loom.Options{
		Backend:     backend,
		SandboxRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return NewMCPHandler(svc), backend
}

func TestHandler_ListTools_FiveVerbs(t *testing.T) {
	h, _ := newTestHandler(t)
	tools := h.ListTools()
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(tools))
	}
	want := map[string]bool{
		ToolLease:   true,
		ToolPush:    true,
		ToolMerge:   true,
		ToolStatus:  true,
		ToolRelease: true,
	}
	for _, tool := range tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool %q", tool.Name)
		}
		// Schema must be valid JSON.
		var anyObj any
		if err := json.Unmarshal(tool.InputSchema, &anyObj); err != nil {
			t.Errorf("tool %s schema invalid: %v", tool.Name, err)
		}
	}
}

func TestHandler_RequiredMode(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := map[string]mcp.PermissionMode{
		ToolLease:   mcp.ModeWorkspaceWrite,
		ToolPush:    mcp.ModeWorkspaceWrite,
		ToolMerge:   mcp.ModeWorkspaceWrite,
		ToolRelease: mcp.ModeWorkspaceWrite,
		ToolStatus:  mcp.ModeReadOnly,
		"unknown":   mcp.ModeReadOnly,
	}
	for tool, want := range cases {
		if got := h.RequiredMode(tool); got != want {
			t.Errorf("RequiredMode(%q)=%s want %s", tool, got, want)
		}
	}
}

func TestHandler_Lease_RoundTrip(t *testing.T) {
	h, _ := newTestHandler(t)
	out, err := h.HandleToolCall(context.Background(), ToolLease, json.RawMessage(`{
		"cwd": "/host/p",
		"sub_agent_label": "extract"
	}`))
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	var got loom.Lease
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode lease: %v\n%s", err, out)
	}
	if got.ID == "" || got.Branch == "" || got.Slug == "" {
		t.Errorf("missing fields: %+v", got)
	}
}

func TestHandler_Lease_RejectsBadInput(t *testing.T) {
	h, _ := newTestHandler(t)
	if _, err := h.HandleToolCall(context.Background(), ToolLease, json.RawMessage(`{}`)); err == nil {
		t.Error("expected validation error")
	}
}

func TestHandler_Status_NoArgs(t *testing.T) {
	h, _ := newTestHandler(t)
	out, err := h.HandleToolCall(context.Background(), ToolStatus, nil)
	if err != nil {
		t.Fatalf("Status (nil input): %v", err)
	}
	var arr []loom.LeaseStatus
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("decode statuses: %v\n%s", err, out)
	}
	if len(arr) != 0 {
		t.Errorf("expected empty array on fresh service, got %+v", arr)
	}
}

func TestHandler_Status_NotFound_ReturnsEmpty(t *testing.T) {
	h, _ := newTestHandler(t)
	out, err := h.HandleToolCall(context.Background(), ToolStatus, json.RawMessage(`{"loom_id":"nonexistent"}`))
	if err != nil {
		t.Fatalf("Status: unexpected error %v", err)
	}
	if !strings.Contains(out, "[]") {
		t.Errorf("expected empty array, got %s", out)
	}
}

func TestHandler_FullFlow(t *testing.T) {
	h, _ := newTestHandler(t)
	ctx := context.Background()

	// Lease
	leaseOut, err := h.HandleToolCall(ctx, ToolLease, json.RawMessage(`{"cwd":"/p","sub_agent_label":"l"}`))
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	var lease loom.Lease
	_ = json.Unmarshal([]byte(leaseOut), &lease)

	// Push
	pushOut, err := h.HandleToolCall(ctx, ToolPush, mustMarshal(loom.PushRequest{LoomID: lease.ID}))
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	var push loom.PushResult
	_ = json.Unmarshal([]byte(pushOut), &push)
	if !push.Pushed {
		t.Errorf("Push: %+v", push)
	}

	// Merge
	mergeOut, err := h.HandleToolCall(ctx, ToolMerge, mustMarshal(loom.MergeRequest{LoomID: lease.ID}))
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var mr loom.MergeResult
	_ = json.Unmarshal([]byte(mergeOut), &mr)
	if mr.PRNumber == 0 {
		t.Errorf("Merge: %+v", mr)
	}

	// Release
	relOut, err := h.HandleToolCall(ctx, ToolRelease, mustMarshal(loom.ReleaseRequest{LoomID: lease.ID}))
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !strings.Contains(relOut, `"released": true`) {
		t.Errorf("Release: %s", relOut)
	}
}

func TestHandler_UnknownTool(t *testing.T) {
	h, _ := newTestHandler(t)
	if _, err := h.HandleToolCall(context.Background(), "loom_bogus", nil); err == nil {
		t.Error("expected unknown-tool error")
	}
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
