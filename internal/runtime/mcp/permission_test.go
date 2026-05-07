package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// stubHandler implements ServerHandler for tests. Its tools' required modes
// are looked up via the in-test PermissionAware shim below.
type stubHandler struct {
	modes map[string]PermissionMode
	calls []string
}

func (s *stubHandler) ListTools() []Tool {
	out := make([]Tool, 0, len(s.modes))
	for name := range s.modes {
		out = append(out, Tool{Name: name})
	}
	return out
}
func (s *stubHandler) ListResources() []Resource { return nil }
func (s *stubHandler) HandleToolCall(_ context.Context, name string, _ json.RawMessage) (string, error) {
	s.calls = append(s.calls, name)
	return "ok", nil
}
func (s *stubHandler) ReadResource(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubHandler) RequiredMode(name string) PermissionMode {
	if m, ok := s.modes[name]; ok {
		return m
	}
	return ModeReadOnly
}

func TestGatedHandler_StaticReadOnlyCeiling(t *testing.T) {
	t.Parallel()
	inner := &stubHandler{
		modes: map[string]PermissionMode{
			"read":  ModeReadOnly,
			"write": ModeWorkspaceWrite,
			"shell": ModeDangerFullAccess,
		},
	}
	gate := StaticGate{Ceiling: ModeReadOnly}
	h := NewGatedHandler(inner, gate)

	if _, err := h.HandleToolCall(context.Background(), "read", nil); err != nil {
		t.Fatalf("expected ReadOnly call to pass, got %v", err)
	}
	if _, err := h.HandleToolCall(context.Background(), "write", nil); err == nil {
		t.Fatalf("expected WorkspaceWrite call to be denied")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("expected permission denied, got %v", err)
	}
	if _, err := h.HandleToolCall(context.Background(), "shell", nil); err == nil {
		t.Fatalf("expected DangerFullAccess call to be denied")
	}

	if got := strings.Join(inner.calls, ","); got != "read" {
		t.Fatalf("expected only 'read' to reach inner handler, got: %q", got)
	}
}

func TestGatedHandler_ListTools_Passthrough(t *testing.T) {
	t.Parallel()
	inner := &stubHandler{modes: map[string]PermissionMode{"a": ModeReadOnly, "b": ModeReadOnly}}
	h := NewGatedHandler(inner, StaticGate{Ceiling: ModeReadOnly})
	if got := len(h.ListTools()); got != 2 {
		t.Fatalf("ListTools should bypass the gate; got %d, want 2", got)
	}
}
