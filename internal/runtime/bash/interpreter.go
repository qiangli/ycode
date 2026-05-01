package bash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

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

	// Build runner options.
	opts := []interp.RunnerOption{
		interp.StdIO(stdin, &stdout, &stderr),
		interp.Env(expand.ListEnviron(os.Environ()...)),
		interp.ExecHandlers(NewSecurityExecHandler(e.permMode, e.killTimeout)),
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
