package ycodecli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
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
	switch action {
	case "model-current":
		ref, err := app.harness.SessionModel(id)
		if err != nil {
			return err
		}
		if boolFlag(inv, "json") {
			return writeJSON(out, map[string]any{"session": id, "model_ref": ref})
		}
		_, err = fmt.Fprintln(out, app.doc.Spec.Models[ref].ID)
		return err
	case "model-use":
		if err := app.harness.SetSessionModel(id, inv.Arguments[0]); err != nil {
			return err
		}
		return writeJSON(out, map[string]any{"session": id, "model_ref": inv.Arguments[0]})
	case "pause":
		if err := app.harness.Pause(ctx, id); err != nil {
			return err
		}
		return writeJSON(out, map[string]any{"session": id, "paused": true})
	case "continue":
		if err := app.harness.Continue(ctx, id); err != nil {
			return err
		}
		return writeJSON(out, map[string]any{"session": id, "continued": true})
	case "compact":
		result, err := app.harness.Compact(ctx, id)
		if err != nil {
			return err
		}
		return writeJSON(out, result)
	case "revert":
		result, err := app.harness.Revert(ctx, id)
		if err != nil {
			return err
		}
		return writeJSON(out, result)
	case "plan", "retry", "btw":
		run := uuid.NewString()
		request := public.RunRequest{SessionID: id, RunID: run, IdempotencyKey: run}
		route := inv.Dispatch
		request.FrontendRef, request.TriggerRef, request.AgentRef = route.FrontendRef, route.TriggerRef, route.AgentRef
		request.Principal = localPrincipal()
		var stream <-chan public.Event
		var err error
		if len(inv.Arguments) > 0 {
			request.Body, err = encodePrompt(strings.Join(inv.Arguments, " "))
			if err != nil {
				return err
			}
		}
		if action == "retry" {
			stream, err = app.harness.Retry(ctx, request)
		} else if action == "btw" {
			stream, err = app.harness.Btw(ctx, request)
		} else {
			// Admission authority comes from this command's declared route.
			stream, err = app.harness.Plan(ctx, request)
		}
		if err != nil {
			return err
		}
		renderer := app.renderer(out)
		for item := range stream {
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
	return errors.New("unsupported session control")
}
