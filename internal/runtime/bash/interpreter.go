package bash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/qiangli/coreutils/pkg/nudge"
	"github.com/qiangli/coreutils/pkg/telemetry"
	coreutilsshell "github.com/qiangli/coreutils/shell"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"

	"github.com/qiangli/ycode/internal/runtime/permission"
)

// InterpreterExecutor runs shell commands using the in-process mvdan/sh interpreter.
type InterpreterExecutor struct {
	session     *ShellSession
	permMode    permission.Mode
	killTimeout time.Duration
}

// NewInterpreterExecutor creates an executor that uses the in-process shell interpreter.
func NewInterpreterExecutor(session *ShellSession, mode permission.Mode) *InterpreterExecutor {
	return &InterpreterExecutor{
		session:     session,
		permMode:    mode,
		killTimeout: 2 * time.Second,
	}
}

// Execute runs a command using the in-process shell interpreter.
func (e *InterpreterExecutor) Execute(ctx context.Context, params ExecParams) (*ExecResult, error) {
	// Parse the command string into an AST.
	prog, err := syntax.NewParser().Parse(strings.NewReader(params.Command), "")
	if err != nil {
		return &ExecResult{
			Stderr:   fmt.Sprintf("parse error: %v", err),
			ExitCode: 2,
		}, nil
	}

	// Determine working directory.
	workDir := params.WorkDir
	if workDir == "" && e.session != nil {
		workDir = e.session.WorkDir()
	}

	// Set up stdout/stderr buffers.
	var stdout, stderr bytes.Buffer

	// Set up stdin.
	var stdin *os.File
	if params.Stdin != "" {
		// Create a pipe for stdin content.
		r, w, pipeErr := os.Pipe()
		if pipeErr != nil {
			return nil, fmt.Errorf("create stdin pipe: %w", pipeErr)
		}
		go func() {
			defer w.Close()
			_, _ = w.WriteString(params.Stdin)
		}()
		stdin = r
		defer r.Close()
	} else {
		stdin, _ = os.Open(os.DevNull)
		defer stdin.Close()
	}

	// THE EXEC CHAIN IS BASHY'S. This is what "embedded bashy" means.
	//
	// ycode used to install a security handler and NOTHING ELSE — so every command it
	// ran forked out to PATH, and it got none of the substrate bashy exists to provide:
	//
	//	no pure-Go userland   — bashy's whole Tier-1 thesis is IN-PROCESS, zero forks
	//	no telemetry          — ycode's commands were invisible on the OTel plane
	//	no advisor, no audit  — the middleware that steers agents off doomed retries
	//
	// The irony was total: bashy ships a `force-agent-shell` skill that ATTESTS that
	// Claude Code, OpenCode and Aider route their shell through bashy — and ycode, the
	// FIRST-PARTY harness, quietly ran its own. It is not even in the adoption matrix
	// it exists to justify.
	//
	// Order is outermost-first: telemetry sees the true wall-clock and final exit of
	// everything below it; security gates before anything executes; the coreutils
	// userland resolves in-process and is innermost, so a pure-Go tool never forks.
	opts := []interp.RunnerOption{
		interp.StdIO(stdin, &stdout, &stderr),
		interp.Env(expand.ListEnviron(os.Environ()...)),
		interp.ExecHandlers(
			telemetry.ExecMiddleware,
			// validate → in-process coreutils → fork. coreutils.Handler() sits
			// BETWEEN validation and the fork so a registered pure-Go tool
			// (grep --json, ast, graph, the userland) resolves in-process and
			// never forks; only a command coreutils declines reaches the fork.
			NewSecurityValidateHandler(e.permMode),
			coreutilsshell.Handler(),
			NewForkExecHandler(e.killTimeout),
		),
		// Proactive hints: when the agent runs a legacy tool with a better
		// composable/structured counterpart (recursive grep → --agentic/--json/
		// ast refs; find → ast symbols), emit ONE rate-limited stderr hint. The
		// process-singleton keeps rate-limiting per-session across each command's
		// own Runner. Observer only — never alters the command; self-silences
		// unless hints are enabled (agent mode / BASHY_HINTS).
		interp.WithAuditHandler(nudge.Default().OnAudit),
	}

	if workDir != "" {
		opts = append(opts, interp.Dir(workDir))
	}

	runner, err := interp.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("create interpreter: %w", err)
	}

	// Run the program.
	runErr := runner.Run(ctx, prog)

	// Extract exit code from error.
	exitCode := 0
	if runErr != nil {
		var es interp.ExitStatus
		if errors.As(runErr, &es) {
			exitCode = int(es)
			runErr = nil
		} else if ctx.Err() != nil {
			// Context cancelled or timed out.
			return &ExecResult{
				Stdout:   truncateOutput(stdout.String()),
				Stderr:   truncateOutput(stderr.String()),
				ExitCode: 130,
			}, fmt.Errorf("command timed out")
		} else {
			// Fatal interpreter error.
			return &ExecResult{
				Stdout:   truncateOutput(stdout.String()),
				Stderr:   truncateOutput(stderr.String()),
				ExitCode: 1,
			}, runErr
		}
	}

	// Update session working directory from the runner.
	if e.session != nil {
		newDir := runner.Dir
		if newDir != "" {
			e.session.SetWorkDir(newDir)
		}
	}

	result := &ExecResult{
		Stdout:   truncateOutput(stdout.String()),
		Stderr:   truncateOutput(stderr.String()),
		ExitCode: exitCode,
	}

	return result, runErr
}
