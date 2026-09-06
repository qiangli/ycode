package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
)

func (r *Runtime) preflight(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	call, err := callFrom(in.Inputs["intent"])
	if err != nil {
		return fail(err)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	report, err := r.bashy.Preflight(ctx, r.bashyMeta(ctx, meta, call.ID), call)
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"report": report})
}

func (r *Runtime) evaluate(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	report, ok := in.Inputs["preflight"].(hitl.Preflight)
	if !ok {
		return fail(fmt.Errorf("policy.evaluate: invalid preflight %T", in.Inputs["preflight"]))
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	decision, err := r.hitl.Evaluate(hitlMeta(meta), text(in.With["policyRef"]), report)
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"decision": decision, "authorization": decision.Binding})
}

func (r *Runtime) execute(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	call, err := callFrom(in.Inputs["intent"])
	if err != nil {
		return fail(err)
	}
	binding := text(in.Inputs["authorization"])
	if binding == "" {
		return fail(errors.New("bashy.execute: missing digest-bound authorization"))
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	result, err := r.bashy.Execute(ctx, r.bashyMeta(ctx, meta, call.ID), call, binding)
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"result": result})
}

func (r *Runtime) deny(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	call, err := callFrom(in.Inputs["intent"])
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"result": map[string]any{"call_id": call.ID, "outcome": "denied"}, "terminal": true})
}

func (r *Runtime) reject(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	call, err := callFrom(in.Inputs["intent"])
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"result": map[string]any{"call_id": call.ID, "outcome": "rejected"}, "terminal": true})
}

func (r *Runtime) commandInitialize(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	call, err := callFrom(in.Inputs["call"])
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"state": map[string]any{
		"intent": call, "terminal": false, "editCount": 0, "maxEdits": intValue(in.With["maxEdits"]), "audit": map[string]any{"call_id": call.ID},
	}})
}

func (r *Runtime) commandFinish(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	state["terminal"] = true
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) commandApplyEdit(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	call, err := callFrom(in.Inputs["editedIntent"])
	if err != nil {
		return fail(err)
	}
	count, _ := number(state["editCount"])
	max, _ := number(state["maxEdits"])
	if count >= max {
		return fail(errors.New("command.apply-edit: compiled edit limit reached"))
	}
	state["intent"], state["editCount"], state["terminal"] = call, count+1, false
	delete(state, "preflight")
	delete(state, "policy")
	delete(state, "authorization")
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) appendToolResults(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	for _, raw := range anyList(in.Inputs["results"]) {
		entry, err := object(raw)
		if err != nil {
			return fail(err)
		}
		result := entry["result"]
		encoded, err := json.Marshal(result)
		if err != nil {
			return fail(err)
		}
		callID := ""
		if value, ok := result.(map[string]any); ok {
			callID = text(value["call_id"])
		}
		messages = append(messages, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: callID, Content: string(encoded)}}})
	}
	state["messages"] = messages
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) review(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	report, ok := in.Inputs["preflight"].(hitl.Preflight)
	if !ok {
		return fail(fmt.Errorf("hitl.review: invalid preflight %T", in.Inputs["preflight"]))
	}
	decision, ok := in.Inputs["decision"].(hitl.Decision)
	if !ok {
		return fail(fmt.Errorf("hitl.review: invalid policy decision %T", in.Inputs["decision"]))
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	review, err := r.hitl.Ask(hitlMeta(meta), hitl.ReviewRequest{PolicyRef: decision.PolicyRef, Report: report, State: in.Inputs["checkpoint"], HumanAvailable: run.humanAvailable})
	if err != nil {
		return fail(err)
	}
	action := "reject"
	if review.Outcome == "waiting" {
		waiter := make(chan hitl.Resolution, 1)
		r.resumeMu.Lock()
		if resolution, ok := r.early[review.DecisionID]; ok {
			delete(r.early, review.DecisionID)
			r.resumeMu.Unlock()
			result := map[string]any{"action": resolution.Action, "binding": resolution.Binding, "review": review}
			if resolution.EditedReport != nil {
				result["editedIntent"] = resolution.EditedReport.Call
			}
			return pipeline.Success(map[string]any{"resolution": result})
		}
		r.resumes[review.DecisionID] = waiter
		r.resumeMu.Unlock()
		defer func() { r.resumeMu.Lock(); delete(r.resumes, review.DecisionID); r.resumeMu.Unlock() }()
		select {
		case resolution := <-waiter:
			result := map[string]any{"action": resolution.Action, "binding": resolution.Binding, "review": review}
			if resolution.EditedReport != nil {
				result["editedIntent"] = resolution.EditedReport.Call
			}
			return pipeline.Success(map[string]any{"resolution": result})
		case <-ctx.Done():
			return pipeline.Failure("hitl-wait-cancelled", false, ctx.Err())
		}
	}
	return pipeline.Success(map[string]any{"resolution": map[string]any{"action": action, "review": review}})
}

func (r *Runtime) bindApproval(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	resolution, err := object(state["resolution"])
	if err != nil {
		return fail(err)
	}
	binding := text(resolution["binding"])
	if binding == "" {
		return fail(errors.New("policy.bind-approval: resolution has no one-use binding"))
	}
	state["authorization"] = binding
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) lockAcquire(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	ref := text(in.With["lockRef"])
	lock, ok := r.doc.Spec.Locks[ref]
	if !ok {
		return fail(fmt.Errorf("lock.acquire: undeclared lock %q", ref))
	}
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"lease": map[string]any{"lockRef": ref, "fencingToken": stableID(run.sessionID, run.runID, in.StageID), "leaseMs": lock.LeaseMS}})
}

func callFrom(value any) (hitl.Call, error) {
	if call, ok := value.(hitl.Call); ok {
		return call, nil
	}
	if outer, ok := value.(map[string]any); ok {
		if input, ok := outer["input"].(map[string]any); ok {
			value = map[string]any{"id": outer["id"], "name": outer["name"], "script": input["script"], "timeout_ms": input["timeout_ms"]}
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return hitl.Call{}, err
	}
	var call hitl.Call
	if err := json.Unmarshal(raw, &call); err != nil {
		return hitl.Call{}, err
	}
	if call.ID == "" || call.Name != "bashy" || call.Script == "" {
		return hitl.Call{}, errors.New("turn: invalid Bashy call")
	}
	return call, nil
}

func hitlMeta(meta ioctx.Meta) hitl.Meta {
	return hitl.Meta{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, ConfigDigest: meta.ConfigDigest}
}

func (r *Runtime) bashyMeta(ctx context.Context, meta ioctx.Meta, callID string) hitl.Meta {
	result := hitlMeta(meta)
	result.IdempotencyKey = callID
	if run, err := runFrom(ctx); err == nil {
		result.PlacementID = r.doc.Spec.Agents[run.agentRef].PlacementRef
	}
	return result
}

func intValue(value any) int { result, _ := number(value); return result }
