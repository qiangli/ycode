package ycodecli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// Session controls are canonical commands; UIs may project these same APIs
// through their live application to retain cancellation and HITL ownership.
func sessionControlCLI(ctx context.Context, app *harnessApplication, inv harnesscli.Invocation, out io.Writer) error {
	id := stringFlag(inv, "session")
	if id == "" {
		id = pointedSession()
	}
	if id == "" {
		sessions, err := app.harness.Sessions()
		if err != nil {
			return err
		}
		if len(sessions) > 0 {
			id = sessions[0].ID
		}
	}
	if id == "" && inv.Dispatch.Action != "model-current" {
		return errors.New("session control requires --session or a current session")
	}
	action := inv.Dispatch.Action
	if boolFlag(inv, "dry-run") {
		return writeJSON(out, map[string]any{"action": action, "session": id, "dry_run": true, "arguments": inv.Arguments})
	}
	result, err := sessionControl(ctx, app, inv.Dispatch, id, localPrincipal(), inv.Arguments)
	if err != nil {
		return err
	}
	if result.Stream == nil {
		if action == "model-current" && !boolFlag(inv, "json") {
			_, err = fmt.Fprintln(out, app.doc.Spec.Models[result.ModelRef].ID)
			return err
		}
		return writeJSON(out, result.Value)
	}
	renderer := app.renderer(out)
	for item := range result.Stream {
		if item.Type == "session.mode-selected" {
			if err := writeJSON(out, item); err != nil {
				return err
			}
			continue
		}
		if boolFlag(inv, "json") {
			if err := writeJSON(out, item); err != nil {
				return err
			}
			if item.Type != "turn.failed" {
				continue
			}
		}
		if err := renderer.Render(item); err != nil {
			return err
		}
	}
	return nil
}

// controlResult is what a session control produced: a stream for the turn
// controls (plan, retry, btw), else a structured value.
type controlResult struct {
	Stream   <-chan public.Event
	Value    any
	ModelRef string // model-current
}

// sessionControl runs one declared session action on session for principal.
// Every frontend that projects a declared session command calls it with
// that command's dispatch, so the authored action and route govern: a turn
// control is admitted under the command's own frontendRef, triggerRef and
// agentRef, never the caller's.
func sessionControl(ctx context.Context, app *harnessApplication, route harnessspec.CLIDispatch, id, principal string, args []string) (controlResult, error) {
	switch route.Action {
	case "model-current":
		ref, err := app.harness.SessionModel(id)
		if err != nil {
			return controlResult{}, err
		}
		return controlResult{Value: map[string]any{"session": id, "model_ref": ref}, ModelRef: ref}, nil
	case "model-use":
		if len(args) != 1 {
			return controlResult{}, errors.New("model-use takes exactly one model resource")
		}
		if err := app.harness.SetSessionModel(id, args[0]); err != nil {
			return controlResult{}, err
		}
		return controlResult{Value: map[string]any{"session": id, "model_ref": args[0]}, ModelRef: args[0]}, nil
	case "pause":
		if err := app.harness.Pause(ctx, id); err != nil {
			return controlResult{}, err
		}
		return controlResult{Value: map[string]any{"session": id, "paused": true}}, nil
	case "continue":
		if err := app.harness.Continue(ctx, id); err != nil {
			return controlResult{}, err
		}
		return controlResult{Value: map[string]any{"session": id, "continued": true}}, nil
	case "compact":
		result, err := app.harness.Compact(ctx, id)
		return controlResult{Value: result}, err
	case "revert":
		result, err := app.harness.Revert(ctx, id)
		return controlResult{Value: result}, err
	case "plan", "retry", "btw":
		run := uuid.NewString()
		request := public.RunRequest{SessionID: id, RunID: run, IdempotencyKey: run}
		request.FrontendRef, request.TriggerRef, request.AgentRef = route.FrontendRef, route.TriggerRef, route.AgentRef
		request.Principal = principal
		var stream <-chan public.Event
		var err error
		if len(args) > 0 {
			request.Body, err = encodePrompt(strings.Join(args, " "))
			if err != nil {
				return controlResult{}, err
			}
		}
		if route.Action == "retry" {
			stream, err = app.harness.Retry(ctx, request)
		} else if route.Action == "btw" {
			stream, err = app.harness.Btw(ctx, request)
		} else {
			// Admission authority comes from this command's declared route.
			stream, err = app.harness.Plan(ctx, request)
		}
		return controlResult{Stream: stream}, err
	}
	return controlResult{}, errors.New("unsupported session control")
}
