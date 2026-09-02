// Package bashy adapts the one model-visible tool to Bashy's embedded runner.
package bashy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	bashyrunner "github.com/qiangli/bashy/pkg/runner"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

// Call is the complete and only model-visible tool input.
type Call struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Script    string `json:"script"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type Result struct {
	CallID          string `json:"call_id"`
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
}

type Executor struct {
	config spec.Bashy
	dir    string
	store  *event.Store
}

func NewExecutor(config spec.Bashy, dir string, store *event.Store) (*Executor, error) {
	if config.ToolName != "bashy" || config.TimeoutMS <= 0 || config.MaxOutputChars <= 0 {
		return nil, errors.New("bashy executor requires the compiled Bashy configuration")
	}
	return &Executor{config: config, dir: dir, store: store}, nil
}

// Execute runs one approved call. Policy review is deliberately a separate
// YAML stage; this method does not invent an approval or retry.
func (e *Executor) Execute(ctx context.Context, sessionID, runID, stageID string, call Call) (Result, error) {
	if call.ID == "" || call.Name != e.config.ToolName || call.Script == "" {
		return Result{}, errors.New("bashy call requires id, name=bashy and script")
	}
	timeout := e.config.TimeoutMS
	if call.TimeoutMS > 0 && call.TimeoutMS < timeout {
		timeout = call.TimeoutMS
	}
	if err := e.append(event.Draft{SessionID: sessionID, RunID: runID, StageID: stageID, Type: "bashy.requested", CallID: call.ID, Data: call}); err != nil {
		return Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	raw := bashyrunner.Run(runCtx, bashyrunner.Request{Script: call.Script, Dir: e.dir, Env: os.Environ(), MaxOutputChars: e.config.MaxOutputChars})
	result := Result{CallID: call.ID, ExitCode: raw.ExitCode, Stdout: raw.Stdout, Stderr: raw.Stderr, StdoutTruncated: raw.StdoutTruncated, StderrTruncated: raw.StderrTruncated}
	typeName := "bashy.completed"
	if raw.ExitCode != 0 || runCtx.Err() != nil {
		typeName = "bashy.failed"
	}
	if err := e.append(event.Draft{SessionID: sessionID, RunID: runID, StageID: stageID, Type: typeName, CallID: call.ID, Data: result}); err != nil {
		return Result{}, err
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return result, fmt.Errorf("bashy call %q timed out after %dms: %w", call.ID, timeout, runCtx.Err())
	}
	return result, nil
}

func (e *Executor) append(draft event.Draft) error {
	if e.store == nil {
		return nil
	}
	_, err := e.store.Append(draft)
	return err
}
