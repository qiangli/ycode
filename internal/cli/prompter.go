package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
// and blocks until the user responds. The mutex ensures that when multiple
// parallel tools need permission, prompts are shown one at a time.
type TUIPrompter struct {
	program *tea.Program
	mu      sync.Mutex
}

// NewTUIPrompter creates a prompter that sends permission requests to the given
// bubbletea program.
func NewTUIPrompter(p *tea.Program) *TUIPrompter {
	return &TUIPrompter{program: p}
}

// Prompt asks the user for permission to execute a tool.
// It sends a message to the TUI and blocks until the user responds y/n.
// The mutex serializes prompts so parallel tool executions don't overlap.
func (tp *TUIPrompter) Prompt(ctx context.Context, toolName string, requiredMode permission.Mode) (bool, error) {
	tracer := otel.Tracer("ycode.permission")
	ctx, span := tracer.Start(ctx, "ycode.permission.prompt",
		trace.WithAttributes(
			attribute.String("tool.name", toolName),
			attribute.String("permission.mode", requiredMode.String()),
		),
	)
	defer span.End()

	mutexStart := time.Now()
	tp.mu.Lock()
	defer tp.mu.Unlock()
	span.SetAttributes(attribute.Int64("permission.mutex_wait_ms", time.Since(mutexStart).Milliseconds()))

	replyCh := make(chan bool, 1)
	tp.program.Send(permissionRequestMsg{
		ToolName:     toolName,
		RequiredMode: requiredMode,
		ReplyCh:      replyCh,
	})

	promptStart := time.Now()
	select {
	case allowed := <-replyCh:
		span.SetAttributes(
			attribute.Bool("permission.allowed", allowed),
			attribute.Int64("permission.wait_ms", time.Since(promptStart).Milliseconds()),
		)
		return allowed, nil
	case <-ctx.Done():
		span.SetAttributes(attribute.String("permission.outcome", "cancelled"))
		return false, fmt.Errorf("permission prompt cancelled: %w", ctx.Err())
	}
}
