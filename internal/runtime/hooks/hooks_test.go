package hooks

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistry_RunNoHandlers(t *testing.T) {
	reg := NewRegistry()
	resp, err := reg.Run(context.Background(), EventPreToolUse, &Event{Name: EventPreToolUse})
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		t.Fatal("expected nil response with no handlers")
	}
}

func TestRegistry_RunContinue(t *testing.T) {
	reg := NewRegistry()
	reg.Register(EventPreToolUse, Registration{
		Handler: &GoHookHandler{
			Fn: func(_ context.Context, _ string, _ *Event) (*HookResponse, error) {
				return &HookResponse{Action: ActionContinue}, nil
			},
		},
	})

	resp, err := reg.Run(context.Background(), EventPreToolUse, &Event{
		Name:     EventPreToolUse,
		ToolName: "bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil && resp.Action == ActionBlock {
		t.Fatal("expected continue, got block")
	}
}

func TestRegistry_RunBlock(t *testing.T) {
	reg := NewRegistry()
	reg.Register(EventPreToolUse, Registration{
		Handler: &GoHookHandler{
			Fn: func(_ context.Context, _ string, _ *Event) (*HookResponse, error) {
				return &HookResponse{
					Action:  ActionBlock,
					Message: "blocked by policy",
				}, nil
			},
		},
	})

	resp, err := reg.Run(context.Background(), EventPreToolUse, &Event{
		Name:     EventPreToolUse,
		ToolName: "bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp == nil || resp.Action != ActionBlock {
		t.Fatal("expected block response")
	}
	if resp.Message != "blocked by policy" {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

func TestRegistry_ToolPatternMatch(t *testing.T) {
	reg := NewRegistry()
	reg.Register(EventPreToolUse, Registration{
		Handler: &GoHookHandler{
			Fn: func(_ context.Context, _ string, _ *Event) (*HookResponse, error) {
				return &HookResponse{Action: ActionBlock, Message: "matched"}, nil
			},
		},
		Match: "bash",
	})

	// Should match "bash".
	resp, _ := reg.Run(context.Background(), EventPreToolUse, &Event{
		Name:     EventPreToolUse,
		ToolName: "bash",
	})
	if resp == nil || resp.Action != ActionBlock {
		t.Fatal("expected block for matching tool")
	}

	// Should NOT match "edit_file".
	resp, _ = reg.Run(context.Background(), EventPreToolUse, &Event{
		Name:     EventPreToolUse,
		ToolName: "edit_file",
	})
	if resp != nil && resp.Action == ActionBlock {
		t.Fatal("expected no match for edit_file")
	}
}

func TestRegistry_WildcardMatch(t *testing.T) {
	reg := NewRegistry()
	reg.Register(EventPreToolUse, Registration{
		Handler: &GoHookHandler{
			Fn: func(_ context.Context, _ string, _ *Event) (*HookResponse, error) {
				return &HookResponse{Action: ActionBlock}, nil
			},
		},
		Match: "git_*",
	})

	// Should match "git_commit".
	resp, _ := reg.Run(context.Background(), EventPreToolUse, &Event{
		Name:     EventPreToolUse,
		ToolName: "git_commit",
	})
	if resp == nil || resp.Action != ActionBlock {
		t.Fatal("expected block for git_commit matching git_*")
	}

	// Should NOT match "bash".
	resp, _ = reg.Run(context.Background(), EventPreToolUse, &Event{
		Name:     EventPreToolUse,
		ToolName: "bash",
	})
	if resp != nil && resp.Action == ActionBlock {
		t.Fatal("expected no match for bash")
	}
}

func TestRegistry_HasHandlers(t *testing.T) {
	reg := NewRegistry()
	if reg.HasHandlers(EventPreToolUse) {
		t.Fatal("expected no handlers initially")
	}

	reg.Register(EventPreToolUse, Registration{
		Handler: &GoHookHandler{
			Fn: func(_ context.Context, _ string, _ *Event) (*HookResponse, error) {
				return nil, nil
			},
		},
	})

	if !reg.HasHandlers(EventPreToolUse) {
		t.Fatal("expected handlers after register")
	}
	if reg.HasHandlers(EventPostToolUse) {
		t.Fatal("expected no handlers for different event")
	}
}

func TestMatchHookPattern(t *testing.T) {
	tests := []struct {
		pattern, tool string
		want          bool
	}{
		{"*", "anything", true},
		{"bash", "bash", true},
		{"bash", "edit_file", false},
		{"git_*", "git_commit", true},
		{"git_*", "git_status", true},
		{"git_*", "bash", false},
		{"edit_*", "edit_file", true},
		{"edit_*", "write_file", false},
	}
	for _, tt := range tests {
		got := matchHookPattern(tt.pattern, tt.tool)
		if got != tt.want {
			t.Errorf("matchHookPattern(%q, %q) = %v, want %v", tt.pattern, tt.tool, got, tt.want)
		}
	}
}

func TestGoHookHandler(t *testing.T) {
	called := false
	handler := &GoHookHandler{
		Fn: func(_ context.Context, event string, payload *Event) (*HookResponse, error) {
			called = true
			if event != EventPostToolUse {
				t.Errorf("expected PostToolUse, got %s", event)
			}
			return &HookResponse{Action: ActionContinue, Message: "logged"}, nil
		},
	}

	resp, err := handler.Execute(context.Background(), EventPostToolUse, &Event{
		Name:     EventPostToolUse,
		ToolName: "bash",
		Output:   "success",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler not called")
	}
	if resp.Message != "logged" {
		t.Errorf("unexpected message: %s", resp.Message)
	}
}

func TestBuildRegistrations(t *testing.T) {
	configs := []HookConfig{
		{Event: EventPreToolUse, Command: "echo test", Timeout: 5000},
		{Event: EventPostToolUse, Command: "echo done"},
	}

	regs := BuildRegistrations(configs)
	if len(regs) != 2 {
		t.Fatalf("expected 2 registrations, got %d", len(regs))
	}
}

func TestHookPayload_JSON(t *testing.T) {
	event := &Event{
		Name:     EventPreToolUse,
		ToolName: "bash",
		Input:    json.RawMessage(`{"command": "ls"}`),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}

	var loaded Event
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.ToolName != "bash" {
		t.Errorf("expected bash, got %s", loaded.ToolName)
	}
}
