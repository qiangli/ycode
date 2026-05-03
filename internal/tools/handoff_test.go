package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestHandoffHandler_BasicHandoff(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:         "Handoff",
		Description:  "Transfer control",
		InputSchema:  json.RawMessage(`{}`),
		RequiredMode: permission.DangerFullAccess,
	})
	RegisterHandoffHandler(r)

	input := json.RawMessage(`{"target_agent": "coder", "message": "fix the bug"}`)
	result, err := r.Invoke(context.Background(), "Handoff", input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"__handoff__":true`) {
		t.Errorf("expected handoff signal, got: %s", result)
	}
	if !strings.Contains(result, `"target_agent":"coder"`) {
		t.Errorf("expected target_agent 'coder', got: %s", result)
	}
}

func TestHandoffHandler_EmptyTarget(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:         "Handoff",
		Description:  "Transfer control",
		InputSchema:  json.RawMessage(`{}`),
		RequiredMode: permission.DangerFullAccess,
	})
	RegisterHandoffHandler(r)

	input := json.RawMessage(`{"target_agent": ""}`)
	_, err := r.Invoke(context.Background(), "Handoff", input)
	if err == nil {
		t.Fatal("expected error for empty target_agent")
	}
}

func TestHandoffHandlerWithAgents_ValidTarget(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:         "Handoff",
		Description:  "Transfer control",
		InputSchema:  json.RawMessage(`{}`),
		RequiredMode: permission.DangerFullAccess,
	})
	RegisterHandoffHandlerWithAgents(r, []string{"coder", "reviewer", "tester"})

	input := json.RawMessage(`{"target_agent": "coder"}`)
	result, err := r.Invoke(context.Background(), "Handoff", input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"target_agent":"coder"`) {
		t.Errorf("expected target_agent 'coder', got: %s", result)
	}
}

func TestHandoffHandlerWithAgents_InvalidTarget(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:         "Handoff",
		Description:  "Transfer control",
		InputSchema:  json.RawMessage(`{}`),
		RequiredMode: permission.DangerFullAccess,
	})
	RegisterHandoffHandlerWithAgents(r, []string{"coder", "reviewer"})

	input := json.RawMessage(`{"target_agent": "hacker"}`)
	_, err := r.Invoke(context.Background(), "Handoff", input)
	if err == nil {
		t.Fatal("expected error for invalid agent name")
	}
	if !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("expected 'unknown agent' error, got: %v", err)
	}
}

func TestHandoffHandlerWithAgents_NilList(t *testing.T) {
	r := NewRegistry()
	r.Register(&ToolSpec{
		Name:         "Handoff",
		Description:  "Transfer control",
		InputSchema:  json.RawMessage(`{}`),
		RequiredMode: permission.DangerFullAccess,
	})
	// nil list = no validation (same as RegisterHandoffHandler)
	RegisterHandoffHandlerWithAgents(r, nil)

	input := json.RawMessage(`{"target_agent": "anything"}`)
	result, err := r.Invoke(context.Background(), "Handoff", input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, `"target_agent":"anything"`) {
		t.Errorf("expected any target to be accepted, got: %s", result)
	}
}
