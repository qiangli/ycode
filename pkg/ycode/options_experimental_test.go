package ycode

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/permission"
	"github.com/qiangli/ycode/internal/tools"
)

// stubProvider is a minimal api.Provider used for tests that need to build
// an Agent but never actually call the network.
type stubProvider struct {
	kind       api.ProviderKind
	lastReq    *api.Request
	streamFunc func(req *api.Request) []*api.StreamEvent
}

func (p *stubProvider) Kind() api.ProviderKind { return p.kind }

func (p *stubProvider) Send(_ context.Context, req *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	p.lastReq = req
	events := make(chan *api.StreamEvent, 8)
	errc := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errc)
		if p.streamFunc != nil {
			for _, ev := range p.streamFunc(req) {
				events <- ev
			}
		}
		events <- &api.StreamEvent{Type: "message_stop"}
	}()
	return events, errc
}

func newStubProvider(kind api.ProviderKind) *stubProvider {
	return &stubProvider{kind: kind}
}

func newStubbedAgent(t *testing.T, opts ...Option) *Agent {
	t.Helper()
	base := []Option{WithProvider(newStubProvider(api.ProviderOpenAI))}
	a, err := NewAgent(append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	return a
}

func TestWithoutBuiltinTools_DropsBash(t *testing.T) {
	a := newStubbedAgent(t, WithoutBuiltinTools())
	if _, ok := a.Registry().Get("bash"); ok {
		t.Error("bash should be absent under WithoutBuiltinTools")
	}
	if _, ok := a.Registry().Get("write_file"); ok {
		t.Error("write_file should be absent under WithoutBuiltinTools")
	}
	if names := a.Registry().Names(); len(names) != 0 {
		t.Errorf("expected zero tools; got %d: %v", len(names), names)
	}
}

func TestWithBuiltinAllowlist_Enforced(t *testing.T) {
	allow := []string{"read_file", "grep_search"}
	a := newStubbedAgent(t, WithBuiltinAllowlist(allow))

	names := a.Registry().Names()
	slices.Sort(names)
	want := []string{"grep_search", "read_file"}
	if !slices.Equal(names, want) {
		t.Errorf("allowlist mismatch:\n  want %v\n  got  %v", want, names)
	}
	if _, ok := a.Registry().Get("bash"); ok {
		t.Error("bash should not be in allowlist build")
	}
}

func TestWithBuiltinAllowlist_EmptyMeansNone(t *testing.T) {
	a := newStubbedAgent(t, WithBuiltinAllowlist([]string{}))
	if names := a.Registry().Names(); len(names) != 0 {
		t.Errorf("empty allowlist should produce zero tools, got %v", names)
	}
}

func TestWithoutBuiltinTools_HostCanRegisterCustom(t *testing.T) {
	a := newStubbedAgent(t, WithoutBuiltinTools())

	called := false
	err := a.Registry().Register(&tools.ToolSpec{
		Name:         "classgo.signoff",
		Description:  "Stub domain tool.",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		RequiredMode: permission.ReadOnly,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			called = true
			return "ok", nil
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := a.Registry().Get("classgo.signoff"); !ok {
		t.Fatal("custom tool not visible after Register")
	}
	if _, err := a.Registry().Invoke(context.Background(), "classgo.signoff", nil); err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !called {
		t.Error("custom tool handler was not invoked")
	}
}

func TestAllowedToolsForMode_PassthroughToBuiltinAllowlist(t *testing.T) {
	// AllowedToolsForAgent(AgentExplore) returns the read-only subagent
	// allowlist. We pass it as the builtin allowlist and assert that every
	// registered tool came from that list (i.e., the registry honors the
	// allowlist exactly) and that write tools are absent.
	allow := AllowedToolsForAgent(AgentExplore)
	a := newStubbedAgent(t, WithBuiltinAllowlist(allow))

	names := a.Registry().Names()
	for _, n := range names {
		if !slices.Contains(allow, n) {
			t.Errorf("registered %q is outside the allowlist %v", n, allow)
		}
	}
	for _, denied := range []string{"bash", "write_file", "edit_file"} {
		if _, ok := a.Registry().Get(denied); ok {
			t.Errorf("write/exec tool %q present despite read-only allowlist", denied)
		}
	}
}
