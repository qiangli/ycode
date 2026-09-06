// Package agent resolves and invokes the agents declared by a compiled harness.
package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type Request struct {
	AgentRef          string
	CallerAgentRef    string
	Input             any
	Depth             int
	PermissionCeiling string
	EffectsCeiling    []string
	SessionID         string
	RunID             string
	CausationID       string
}

type Result struct {
	AgentRef string
	Output   any
	Outcome  pipeline.Outcome
}

// StageInput carries invocation metadata alongside the agent's declared input.
// Frontends construct it mechanically; selection and authority remain YAML data.
type StageInput struct {
	Input          any
	CallerAgentRef string
	Depth          int
	SessionID      string
	RunID          string
	CausationID    string
}

type Roster struct {
	doc    *spec.Document
	runner *pipeline.Runner
	events *event.Store
	mu     sync.Mutex
	limits map[string]chan struct{}
}

func New(doc *spec.Document, runner *pipeline.Runner, events *event.Store) (*Roster, error) {
	if doc == nil || runner == nil || events == nil {
		return nil, errors.New("agent roster requires compiled document, pipeline runner and event store")
	}
	return &Roster{doc: doc, runner: runner, events: events, limits: make(map[string]chan struct{})}, nil
}

// Definition exposes agent.invoke as an ordinary stage backed by this roster.
// It recursively enters the target's configured pipeline, never a subagent-only loop.
func (r *Roster) Definition() pipeline.Definition {
	return pipeline.Definition{
		Name: "agent.invoke", Inputs: map[string]string{"input": "ycode.input/v1"}, Outputs: map[string]string{"output": "ycode.output/v1"},
		Handler: func(ctx context.Context, invocation pipeline.Invocation) pipeline.Outcome {
			ref, _ := invocation.With["agentRef"].(string)
			input, ok := invocation.Inputs["input"].(StageInput)
			if !ok {
				return pipeline.Failure("invalid-agent-input", false, errors.New("agent.invoke requires agent.StageInput"))
			}
			result, err := r.Invoke(ctx, Request{AgentRef: ref, CallerAgentRef: input.CallerAgentRef, Input: input.Input, Depth: input.Depth, SessionID: input.SessionID, RunID: input.RunID, CausationID: input.CausationID})
			if err != nil {
				return pipeline.Failure("agent-invoke", false, err)
			}
			return pipeline.Success(map[string]any{"output": result.Output})
		},
	}
}

func (r *Roster) Invoke(ctx context.Context, request Request) (Result, error) {
	if request.AgentRef == "" || request.SessionID == "" || request.RunID == "" {
		return Result{}, errors.New("agent invocation requires agent, session and run identifiers")
	}
	target, ok := r.doc.Spec.Agents[request.AgentRef]
	if !ok {
		return Result{}, fmt.Errorf("agent %q is not declared", request.AgentRef)
	}

	var delegation *spec.Delegation
	if request.CallerAgentRef != "" {
		caller, exists := r.doc.Spec.Agents[request.CallerAgentRef]
		if !exists {
			return Result{}, fmt.Errorf("caller agent %q is not declared", request.CallerAgentRef)
		}
		for i := range caller.Delegations {
			if caller.Delegations[i].AgentRef == request.AgentRef {
				delegation = &caller.Delegations[i]
				break
			}
		}
		if delegation == nil {
			return Result{}, fmt.Errorf("agent %q cannot delegate to %q", request.CallerAgentRef, request.AgentRef)
		}
		if request.Depth <= 0 || request.Depth > delegation.MaxDepth {
			return Result{}, fmt.Errorf("delegation depth %d exceeds configured maximum %d", request.Depth, delegation.MaxDepth)
		}
		if !permissionWithin(target.PermissionCeiling, delegation.PermissionCeiling) || !effectsWithin(target.EffectsCeiling, delegation.EffectsCeiling) {
			return Result{}, errors.New("delegated agent authority exceeds delegation ceiling")
		}
		if request.PermissionCeiling != "" && !permissionWithin(request.PermissionCeiling, delegation.PermissionCeiling) {
			return Result{}, errors.New("requested permission exceeds delegation ceiling")
		}
		if len(request.EffectsCeiling) != 0 && !effectsWithin(request.EffectsCeiling, delegation.EffectsCeiling) {
			return Result{}, errors.New("requested effects exceed delegation ceiling")
		}
	}

	release, err := r.acquire(ctx, request.CallerAgentRef, request.AgentRef, delegation)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if delegation != nil && delegation.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(delegation.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	data := map[string]any{"agent_ref": request.AgentRef, "caller_agent_ref": request.CallerAgentRef, "depth": request.Depth}
	started, err := r.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, Type: eventType(request, "agent.started", "agent.handoff.started"), CausationID: request.CausationID, ConfigDigest: r.doc.ConfigDigest, Data: data})
	if err != nil {
		return Result{}, err
	}
	state := pipeline.NewState()
	p := r.doc.Spec.Pipelines[target.PipelineRef]
	inputName, inputType, err := solePort(p.Inputs, "input")
	if err != nil {
		return Result{}, fmt.Errorf("agent %q pipeline input: %w", request.AgentRef, err)
	}
	if err := state.SetTyped(inputName, inputType, request.Input); err != nil {
		return Result{}, err
	}
	outcome := r.runner.RunPipelineOutcome(ctx, target.PipelineRef, state)
	if outcome.Class == pipeline.OutcomeFailed || outcome.Class == pipeline.OutcomeCancelled {
		_, _ = r.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, Type: eventType(request, "agent.failed", "agent.handoff.failed"), CausationID: started.Digest, ConfigDigest: r.doc.ConfigDigest, Data: map[string]any{"agent_ref": request.AgentRef, "class": outcome.Class, "code": outcome.Code}})
		if outcome.Err != nil {
			return Result{AgentRef: request.AgentRef, Outcome: outcome}, outcome.Err
		}
		return Result{AgentRef: request.AgentRef, Outcome: outcome}, fmt.Errorf("agent %q failed: %s", request.AgentRef, outcome.Code)
	}
	outputName, _, err := solePort(p.Outputs, "output")
	if err != nil {
		return Result{}, fmt.Errorf("agent %q pipeline output: %w", request.AgentRef, err)
	}
	output, _, _, ok := state.Read(outputName)
	if !ok {
		return Result{}, fmt.Errorf("agent %q did not produce %q", request.AgentRef, outputName)
	}
	_, err = r.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, Type: eventType(request, "agent.completed", "agent.handoff.completed"), CausationID: started.Digest, ConfigDigest: r.doc.ConfigDigest, Data: map[string]any{"agent_ref": request.AgentRef}})
	return Result{AgentRef: request.AgentRef, Output: output, Outcome: outcome}, err
}

func (r *Roster) acquire(ctx context.Context, caller, target string, d *spec.Delegation) (func(), error) {
	if d == nil {
		return func() {}, nil
	}
	key := caller + "->" + target
	r.mu.Lock()
	sem := r.limits[key]
	if sem == nil {
		sem = make(chan struct{}, d.MaxParallel)
		r.limits[key] = sem
	}
	r.mu.Unlock()
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func eventType(request Request, root, delegated string) string {
	if request.CallerAgentRef != "" {
		return delegated
	}
	return root
}

func solePort(ports map[string]string, preferred string) (string, string, error) {
	if typ, ok := ports[preferred]; ok {
		return preferred, typ, nil
	}
	if len(ports) != 1 {
		keys := make([]string, 0, len(ports))
		for key := range ports {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return "", "", fmt.Errorf("expected one %q port, got %v", preferred, keys)
	}
	for name, typ := range ports {
		return name, typ, nil
	}
	panic("unreachable")
}

func effectsWithin(have, ceiling []string) bool {
	allowed := make(map[string]struct{}, len(ceiling))
	for _, effect := range ceiling {
		allowed[effect] = struct{}{}
	}
	for _, effect := range have {
		if _, ok := allowed[effect]; !ok {
			return false
		}
	}
	return true
}

func permissionWithin(have, ceiling string) bool {
	rank := map[string]int{"none": 0, "read-only": 1, "workspace-write": 2, "host-write": 3, "unrestricted": 4}
	h, hok := rank[have]
	c, cok := rank[ceiling]
	return hok && cok && h <= c
}
