package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/qiangli/ycode/internal/runtime/bash"
)

// ttyExecRequestMsg is sent to the TUI when a command needs interactive
// terminal access (e.g., ssh, sudo). The TUI suspends itself and gives
// the process full terminal control via tea.ExecProcess.
type ttyExecRequestMsg struct {
	Command  string
	WorkDir  string
	ResultCh chan ttyExecResult
}

// ttyExecResult is sent back from the TUI after the interactive process exits.
type ttyExecResult struct {
	ExitCode int
	Output   string // captured terminal output (may be empty)
	Err      error
}

// ttyExecDoneMsg is the tea.Msg emitted after tea.ExecProcess completes,
// carrying the result channel so the TUI can unblock the waiting tool goroutine.
type ttyExecDoneMsg struct {
	ResultCh   chan ttyExecResult
	ExitCode   int
	Err        error
	ScriptFile string // path to script capture file (cleaned up after reading)
}

// TUITTYExecutor bridges the agent's bash tool with the bubbletea TUI
// for commands that need interactive terminal access. It follows the same
// channel-based pattern as TUIPrompter.
type TUITTYExecutor struct {
	program *tea.Program
	mu      sync.Mutex
}

// NewTUITTYExecutor creates a TTY executor that delegates to the given
// bubbletea program for interactive command execution.
func NewTUITTYExecutor(p *tea.Program) *TUITTYExecutor {
	return &TUITTYExecutor{program: p}
}

// ExecuteTTY sends an interactive command to the TUI for execution with
// full terminal access. It blocks until the command completes or the
// context is cancelled. Only one TTY command can run at a time.
func (te *TUITTYExecutor) ExecuteTTY(ctx context.Context, command string, workDir string) (*bash.ExecResult, error) {
	tracer := otel.Tracer("ycode.tty")
	ctx, span := tracer.Start(ctx, "ycode.tty.exec",
		trace.WithAttributes(
			attribute.String("command", command),
		),
	)
	defer span.End()

	mutexStart := time.Now()
	te.mu.Lock()
	defer te.mu.Unlock()
	span.SetAttributes(attribute.Int64("tty.mutex_wait_ms", time.Since(mutexStart).Milliseconds()))

	resultCh := make(chan ttyExecResult, 1)
	te.program.Send(ttyExecRequestMsg{
		Command:  command,
		WorkDir:  workDir,
		ResultCh: resultCh,
	})

	execStart := time.Now()
	select {
	case res := <-resultCh:
		span.SetAttributes(
			attribute.Int("tty.exit_code", res.ExitCode),
			attribute.Int64("tty.duration_ms", time.Since(execStart).Milliseconds()),
		)
		if res.Err != nil {
			return nil, res.Err
		}
		stdout := res.Output
		if stdout == "" {
			stdout = fmt.Sprintf("(interactive command exited with code %d)", res.ExitCode)
		} else {
			stdout = bash.TruncateOutput(stdout, bash.MaxOutputSize)
		}
		return &bash.ExecResult{
			Stdout:   stdout,
			ExitCode: res.ExitCode,
		}, nil
	case <-ctx.Done():
		span.SetAttributes(attribute.String("tty.outcome", "cancelled"))
		return nil, fmt.Errorf("TTY execution cancelled: %w", ctx.Err())
	}
}

// ttyCommandWithCapture wraps a command with the `script` utility to capture
// terminal output to a temporary file while still giving the process full TTY
// access. Returns the script file path and the exec.Cmd to run.
//
// On macOS: script -q <file> sh -c <cmd>
// On Linux: script -q -c <cmd> <file>
func ttyCommandWithCapture(command string, workDir string) (string, *exec.Cmd) {
	scriptFile, err := os.CreateTemp("", "ycode-tty-*.txt")
	if err != nil {
		// Fallback: run without capture.
		cmd := exec.Command("sh", "-c", command)
		if workDir != "" {
			cmd.Dir = workDir
		}
		return "", cmd
	}
	scriptPath := scriptFile.Name()
	scriptFile.Close()

	// macOS and Linux have different `script` flags.
	// macOS: script -q <file> <cmd...>
	// Linux: script -q -c <cmd> <file>
	var cmd *exec.Cmd
	if isDarwin() {
		cmd = exec.Command("script", "-q", scriptPath, "sh", "-c", command)
	} else {
		cmd = exec.Command("script", "-q", "-c", command, scriptPath)
	}
	if workDir != "" {
		cmd.Dir = workDir
	}
	return scriptPath, cmd
}

// isDarwin returns true if running on macOS.
func isDarwin() bool {
	return runtime.GOOS == "darwin"
}

// readAndCleanScriptFile reads a script capture file and removes it.
// Returns empty string on any error.
func readAndCleanScriptFile(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	os.Remove(path)
	if err != nil {
		return ""
	}
	return string(data)
}
