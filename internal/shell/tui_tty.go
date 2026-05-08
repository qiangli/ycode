package shell

import (
	"context"
	"errors"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/qiangli/ycode/internal/runtime/bash"
)

// tuiTTYRequestMsg is sent by tuiTTYRunner.RunTTY to the Bubble Tea
// program when an external command needs a controlling terminal. The
// program responds by calling tea.ExecProcess, which suspends Bubble
// Tea, runs the child with full TTY access, and sends a tuiTTYDoneMsg
// when the child exits.
type tuiTTYRequestMsg struct {
	cmd      *exec.Cmd
	resultCh chan tuiTTYResult
}

type tuiTTYResult struct {
	exit int
	err  error
}

// tuiTTYDoneMsg unblocks the runner goroutine that's waiting on
// resultCh. The TUI returns it from the tea.ExecProcess callback.
type tuiTTYDoneMsg struct {
	resultCh chan tuiTTYResult
	exit     int
	err      error
}

// tuiTTYRunner implements bash.TTYRunner by trampolining through the
// Bubble Tea program's tea.ExecProcess flow. The TUI installs one of
// these on the shell session at startup; --no-tui mode uses PTYManager
// instead.
type tuiTTYRunner struct {
	prog *tea.Program
}

// NewTUITTYRunner is the public constructor used by the cobra command
// after it has the *tea.Program. Wire it into the shell session via
// SetTTYRunner before calling prog.Run().
func NewTUITTYRunner(p *tea.Program) bash.TTYRunner { return &tuiTTYRunner{prog: p} }

// RunTTY implements bash.TTYRunner. It builds an exec.Cmd that the TUI
// will hand to tea.ExecProcess, then blocks on a result channel until
// the child exits or ctx is cancelled.
func (r *tuiTTYRunner) RunTTY(ctx context.Context, argv, env []string, cwd string) (int, error) {
	if r == nil || r.prog == nil {
		return 1, errors.New("tuiTTYRunner: no program attached")
	}
	if len(argv) == 0 {
		return 1, errors.New("tuiTTYRunner: empty argv")
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Dir = cwd

	resultCh := make(chan tuiTTYResult, 1)
	r.prog.Send(tuiTTYRequestMsg{cmd: cmd, resultCh: resultCh})

	select {
	case res := <-resultCh:
		return res.exit, res.err
	case <-ctx.Done():
		// Best effort: if the program never picked up the request,
		// just kill the process. Generally this branch is rare because
		// tea.ExecProcess runs synchronously inside the TUI.
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return 130, ctx.Err()
	}
}
