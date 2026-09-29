package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

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
	// Tool results travel as plain objects carrying the call id, the same
	// shape as the denied and rejected paths, so the transcript can link them.
	fields, err := toolResultObject(result)
	if err != nil {
		return fail(err)
	}
	fields["call_id"] = call.ID
	return pipeline.Success(map[string]any{"result": fields})
}

func (r *Runtime) deny(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	call, err := callFrom(in.Inputs["intent"])
	if err != nil {
		return fail(err)
	}
	result := map[string]any{"call_id": call.ID, "outcome": "denied"}
	// with.reason names the policy rule that denied the call, so the model
	// can tell an unprovable command from a network or destructive one.
	if reason, _ := in.With["reason"].(bool); reason {
		if decision, ok := in.Inputs["decision"].(hitl.Decision); ok {
			result["policy_ref"], result["rule_id"] = decision.PolicyRef, decision.RuleID
		}
		// What the preflight could not prove — a parse error, a dynamic
		// command — is the concrete reason the model can act on.
		if report, ok := in.Inputs["preflight"].(hitl.Preflight); ok && len(report.Unsupported) > 0 {
			result["unsupported"] = report.Unsupported
		}
	}
	remedy, err := denialRemedy(in.With["remedy"], in.Inputs["decision"], in.Inputs["preflight"], call)
	if err != nil {
		return fail(err)
	}
	if remedy != "" {
		result["remedy"] = remedy
	}
	return pipeline.Success(map[string]any{"result": result, "terminal": true})
}

// denialRemedy renders with.remedy, the agent's own answer to a denial it
// knows how to fix: the ready-to-run form of the denied command, so the
// model's next call is that form instead of a probe and a rewrite. The
// agent's YAML owns the whole policy:
//
//	remedy:
//	  rules: [incomplete-preflight]   # rule ids it answers
//	  kinds: [command, ...]           # optional: every unsupported fact's kind is one of these
//	  unlessScriptMatches: '^@effects' # optional: a script already in that form gets none
//	  text: "...{{command}}..."       # the model's script, verbatim
//
// It never changes the decision: the command still did not run, and the
// remedy's form goes through preflight and policy like any other call.
func denialRemedy(config, decision, preflight any, call hitl.Call) (string, error) {
	cfg, _ := config.(map[string]any)
	if cfg == nil {
		return "", nil
	}
	template := text(cfg["text"])
	if template == "" {
		return "", errors.New("bashy.deny: with.remedy requires text")
	}
	d, ok := decision.(hitl.Decision)
	if !ok || !containsString(stringList(cfg["rules"]), d.RuleID) {
		return "", nil
	}
	if kinds := stringList(cfg["kinds"]); len(kinds) > 0 {
		report, _ := preflight.(hitl.Preflight)
		if len(report.Unsupported) == 0 {
			return "", nil
		}
		for _, fact := range report.Unsupported {
			if !containsString(kinds, fact.Kind) {
				return "", nil
			}
		}
	}
	if pattern := text(cfg["unlessScriptMatches"]); pattern != "" {
		re, err := regexp.Compile("(?m)" + pattern)
		if err != nil {
			return "", fmt.Errorf("bashy.deny: with.remedy.unlessScriptMatches: %w", err)
		}
		if re.MatchString(call.Script) {
			return "", nil
		}
	}
	return strings.ReplaceAll(template, "{{command}}", strings.TrimRight(call.Script, "\n")), nil
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
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
		// forEach collects each iteration's result value directly; accept a
		// {"result": …} wrapper too.
		result := raw
		if entry, ok := raw.(map[string]any); ok {
			if inner, wrapped := entry["result"]; wrapped {
				result = inner
			}
		}
		fields, err := toolResultObject(result)
		if err != nil {
			return fail(err)
		}
		content := ""
		if text(in.With["format"]) == "observation" {
			content = observationText(fields)
		} else {
			encoded, err := json.Marshal(fields)
			if err != nil {
				return fail(err)
			}
			content = string(encoded)
		}
		callID := text(fields["call_id"])
		messages = append(messages, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeToolResult, ToolUseID: callID, Content: content}}})
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
	if call.ID == "" {
		return hitl.Call{}, errors.New("turn: invalid Bashy call")
	}
	switch {
	case call.Name != "bashy":
		call.Invalid = fmt.Sprintf("there is no tool %q: the only tool is bashy, called with {\"script\": \"<shell commands>\"}", call.Name)
	case strings.TrimSpace(call.Script) == "":
		call.Invalid = "the bashy call has no script: pass the shell commands to run as {\"script\": \"...\"}"
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

// toolResultObject normalises a tool result (a map or a typed result) into a
// JSON object.
func toolResultObject(result any) (map[string]any, error) {
	if value, ok := result.(map[string]any); ok {
		copy := make(map[string]any, len(value))
		for key, item := range value {
			copy[key] = item
		}
		return copy, nil
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("turn: encode tool result: %w", err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("turn: tool result is not an object: %w", err)
	}
	return fields, nil
}
