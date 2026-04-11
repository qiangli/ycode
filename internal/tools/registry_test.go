package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

func TestInvoke_NoPermissionResolver(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "test_tool",
		RequiredMode: permission.WorkspaceWrite,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "ok", nil
		},
	})

	// Without a resolver, all tools should be allowed.
	result, err := r.Invoke(context.Background(), "test_tool", nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}

func TestInvoke_PermissionDeniedInPlanMode(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "write_file",
		RequiredMode: permission.WorkspaceWrite,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "written", nil
		},
	})

	// Set resolver to return ReadOnly (plan mode).
	r.SetPermissionResolver(func() permission.Mode {
		return permission.ReadOnly
	})

	_, err := r.Invoke(context.Background(), "write_file", nil)
	if err == nil {
		t.Fatal("expected permission denied error")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected 'permission denied' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "plan mode") {
		t.Errorf("expected 'plan mode' hint in error, got: %v", err)
	}
}

func TestInvoke_ReadToolAllowedInPlanMode(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "read_file",
		RequiredMode: permission.ReadOnly,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "file content", nil
		},
	})

	// Plan mode = ReadOnly.
	r.SetPermissionResolver(func() permission.Mode {
		return permission.ReadOnly
	})

	result, err := r.Invoke(context.Background(), "read_file", nil)
	if err != nil {
		t.Fatalf("expected read_file to be allowed in plan mode, got: %v", err)
	}
	if result != "file content" {
		t.Errorf("expected 'file content', got %q", result)
	}
}

func TestInvoke_PermissionPrompterApproves(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "edit_file",
		RequiredMode: permission.WorkspaceWrite,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "edited", nil
		},
	})

	// Plan mode, but with a prompter that always approves.
	r.SetPermissionResolver(func() permission.Mode {
		return permission.ReadOnly
	})

	prompted := false
	r.SetPermissionPrompter(func(ctx context.Context, toolName string, requiredMode permission.Mode) (bool, error) {
		prompted = true
		if toolName != "edit_file" {
			t.Errorf("expected tool name 'edit_file', got %q", toolName)
		}
		return true, nil
	})

	result, err := r.Invoke(context.Background(), "edit_file", nil)
	if err != nil {
		t.Fatalf("expected no error after approval, got: %v", err)
	}
	if result != "edited" {
		t.Errorf("expected 'edited', got %q", result)
	}
	if !prompted {
		t.Error("expected prompter to be called")
	}
}

func TestInvoke_PermissionPrompterDenies(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "bash",
		RequiredMode: permission.DangerFullAccess,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "executed", nil
		},
	})

	r.SetPermissionResolver(func() permission.Mode {
		return permission.ReadOnly
	})
	r.SetPermissionPrompter(func(ctx context.Context, toolName string, requiredMode permission.Mode) (bool, error) {
		return false, nil // user denies
	})

	_, err := r.Invoke(context.Background(), "bash", nil)
	if err == nil {
		t.Fatal("expected permission denied error after user denial")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected 'permission denied' in error, got: %v", err)
	}
}

func TestInvoke_WriteToolsAllowedInBuildMode(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "write_file",
		RequiredMode: permission.WorkspaceWrite,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "written", nil
		},
	})

	// Build mode = WorkspaceWrite.
	r.SetPermissionResolver(func() permission.Mode {
		return permission.WorkspaceWrite
	})

	result, err := r.Invoke(context.Background(), "write_file", nil)
	if err != nil {
		t.Fatalf("expected write_file to be allowed in build mode, got: %v", err)
	}
	if result != "written" {
		t.Errorf("expected 'written', got %q", result)
	}
}

func TestInvoke_DangerToolDeniedInBuildMode(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&ToolSpec{
		Name:         "bash",
		RequiredMode: permission.DangerFullAccess,
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "executed", nil
		},
	})

	// Build mode = WorkspaceWrite (not enough for DangerFullAccess).
	r.SetPermissionResolver(func() permission.Mode {
		return permission.WorkspaceWrite
	})

	_, err := r.Invoke(context.Background(), "bash", nil)
	if err == nil {
		t.Fatal("expected bash to be denied in workspace-write mode")
	}
}
