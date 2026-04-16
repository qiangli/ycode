package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFilteredRegistry_AllowsSubset(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:            "bash",
		Description:     "Execute bash",
		InputSchema:     json.RawMessage(`{}`),
		AlwaysAvailable: true,
		Handler:         func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil },
	})
	_ = reg.Register(&ToolSpec{
		Name:            "write_file",
		Description:     "Write a file",
		InputSchema:     json.RawMessage(`{}`),
		AlwaysAvailable: true,
		Handler:         func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil },
	})
	_ = reg.Register(&ToolSpec{
		Name:            "read_file",
		Description:     "Read a file",
		InputSchema:     json.RawMessage(`{}`),
		AlwaysAvailable: true,
		Handler:         func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil },
	})

	fr := NewFilteredRegistry(reg, []string{"bash", "read_file"})

	// Allowed tool.
	if _, ok := fr.Get("bash"); !ok {
		t.Error("bash should be allowed")
	}
	// Blocked tool.
	if _, ok := fr.Get("write_file"); ok {
		t.Error("write_file should be blocked")
	}
	// Invoke blocked tool.
	_, err := fr.Invoke(context.Background(), "write_file", nil)
	if err == nil {
		t.Error("invoking blocked tool should error")
	}
	// Names should only include allowed.
	names := fr.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d: %v", len(names), names)
	}
}

func TestFilteredRegistry_NilAllowlist(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:        "bash",
		InputSchema: json.RawMessage(`{}`),
		Handler:     func(ctx context.Context, input json.RawMessage) (string, error) { return "ok", nil },
	})

	fr := NewFilteredRegistry(reg, nil)
	if _, ok := fr.Get("bash"); !ok {
		t.Error("nil allowlist should allow all tools")
	}
}

func TestFilteredRegistry_HideUnhide(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Register(&ToolSpec{
		Name:            "bash",
		Description:     "run bash",
		InputSchema:     json.RawMessage(`{}`),
		AlwaysAvailable: true,
	})
	_ = reg.Register(&ToolSpec{
		Name:            "read_file",
		Description:     "read a file",
		InputSchema:     json.RawMessage(`{}`),
		AlwaysAvailable: true,
	})

	fr := NewFilteredRegistry(reg, nil)

	// Both visible initially.
	if _, ok := fr.Get("bash"); !ok {
		t.Error("bash should be visible")
	}
	if _, ok := fr.Get("read_file"); !ok {
		t.Error("read_file should be visible")
	}

	// Hide bash.
	fr.Hide("bash")
	if _, ok := fr.Get("bash"); ok {
		t.Error("bash should be hidden after Hide")
	}
	if _, ok := fr.Get("read_file"); !ok {
		t.Error("read_file should still be visible")
	}

	// AlwaysAvailable should exclude hidden tools.
	always := fr.AlwaysAvailable()
	for _, s := range always {
		if s.Name == "bash" {
			t.Error("hidden tool should not appear in AlwaysAvailable")
		}
	}

	// Unhide bash.
	fr.Unhide("bash")
	if _, ok := fr.Get("bash"); !ok {
		t.Error("bash should be visible after Unhide")
	}
}
