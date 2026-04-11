package cli

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// permissionRequestMsg is sent to the TUI when a tool needs permission approval.
type permissionRequestMsg struct {
	ToolName     string
	RequiredMode permission.Mode
	ReplyCh      chan bool
}

// TUIPrompter bridges tool permission checks with the bubbletea TUI.
// When a tool requires elevated permissions, it sends a message to the TUI
// and blocks until the user responds.
type TUIPrompter struct {
	program *tea.Program
}

// NewTUIPrompter creates a prompter that sends permission requests to the given
// bubbletea program.
func NewTUIPrompter(p *tea.Program) *TUIPrompter {
	return &TUIPrompter{program: p}
}

// Prompt asks the user for permission to execute a tool.
// It sends a message to the TUI and blocks until the user responds y/n.
func (tp *TUIPrompter) Prompt(ctx context.Context, toolName string, requiredMode permission.Mode) (bool, error) {
	replyCh := make(chan bool, 1)
	tp.program.Send(permissionRequestMsg{
		ToolName:     toolName,
		RequiredMode: requiredMode,
		ReplyCh:      replyCh,
	})

	select {
	case allowed := <-replyCh:
		return allowed, nil
	case <-ctx.Done():
		return false, fmt.Errorf("permission prompt cancelled: %w", ctx.Err())
	}
}
