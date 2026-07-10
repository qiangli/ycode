package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fixedToolsHandler exposes a fixed tool list — used to simulate a transport's
// registered tool set without dragging in real handlers.
type fixedToolsHandler struct{ tools []Tool }

func (s fixedToolsHandler) ListTools() []Tool         { return s.tools }
func (s fixedToolsHandler) ListResources() []Resource { return nil }
func (s fixedToolsHandler) HandleToolCall(context.Context, string, json.RawMessage) (string, error) {
	return "", nil
}
func (s fixedToolsHandler) ReadResource(context.Context, string) (string, error) { return "", nil }

func TestUnknownToolErr_CrossTransportHint(t *testing.T) {
	// Compose with only the docs tools registered. Asking for loom_lease
	// — which crossTransportTools says lives on "http" — should produce
	// the cross-transport hint.
	c := NewCompositeHandler(fixedToolsHandler{tools: []Tool{{Name: "list_docs"}}})
	c.SetTransport("stdio")

	_, err := c.HandleToolCall(context.Background(), "loom_lease", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
	msg := err.Error()
	for _, want := range []string{
		`loom_lease`,
		`transport "stdio"`,
		`available on "http"`,
		`ycode serve`,
		`ycode pair --tool loom_lease`,
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q in error, got: %s", want, msg)
		}
	}
}

func TestUnknownToolErr_NoHintWhenTransportUnset(t *testing.T) {
	c := NewCompositeHandler(fixedToolsHandler{tools: []Tool{{Name: "list_docs"}}})
	// no SetTransport call

	_, err := c.HandleToolCall(context.Background(), "loom_lease", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "available on") {
		t.Fatalf("did not expect cross-transport hint when transport unset, got: %s", err.Error())
	}
	if !strings.HasPrefix(err.Error(), "unknown tool: ") {
		t.Fatalf("expected legacy prefix, got: %s", err.Error())
	}
}

func TestUnknownToolErr_NoHintForUnknownToolName(t *testing.T) {
	// A tool name not in crossTransportTools should not get a hint
	// even if transport is set.
	c := NewCompositeHandler(fixedToolsHandler{tools: []Tool{{Name: "list_docs"}}})
	c.SetTransport("stdio")

	_, err := c.HandleToolCall(context.Background(), "no_such_tool", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "available on") {
		t.Fatalf("did not expect hint for unmapped tool, got: %s", err.Error())
	}
}

func TestUnknownToolErr_NoHintWhenSameTransport(t *testing.T) {
	// loom_lease is owned by "http" — if the composite's transport
	// already matches, no cross-transport hint should fire (the tool
	// is missing for some other reason — e.g. handler not registered).
	c := NewCompositeHandler(fixedToolsHandler{tools: []Tool{{Name: "list_docs"}}})
	c.SetTransport("http")

	_, err := c.HandleToolCall(context.Background(), "loom_lease", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "available on") {
		t.Fatalf("did not expect cross hint when transport matches owner, got: %s", err.Error())
	}
}
