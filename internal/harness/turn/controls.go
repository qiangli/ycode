package turn

import (
	"context"
	"errors"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
)

// plan deliberately bypasses the execution graph: even harness-authored
// bashy.run nodes cannot execute in a text-only planning request.
func (r *Runtime) plan(ctx context.Context, request Request, agent spec.Agent) (ioctx.Output, error) {
	if agent.SessionControls.PlanPrompt == "" || len(agent.SessionControls.PlanSinkRefs) == 0 {
		return ioctx.Output{}, errors.New("planning is not declared for this agent")
	}
	if request.Boundary != nil {
		if err := request.Boundary(ctx); err != nil {
			return ioctx.Output{}, err
		}
	}
	events, err := event.Replay(r.eventPath)
	if err != nil {
		return ioctx.Output{}, err
	}
	meta, err := r.meta(ctx, "session.plan")
	if err != nil {
		return ioctx.Output{}, err
	}
	history, err := r.session.Load(ctx, sessionStage.Meta(meta), sessionStage.LoadRequest{SessionRef: agent.SessionRef, SessionID: request.SessionID, Events: events})
	if err != nil {
		return ioctx.Output{}, err
	}
	messages := append(history.Messages, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: string(request.Input.Data)}}})
	prompt := agent.SessionControls.PlanPrompt
	if request.Aside {
		prompt = agent.SessionControls.BtwPrompt
		if prompt == "" {
			return ioctx.Output{}, errors.New("side queries are not declared")
		}
	}
	response, outcome := r.routeProvider(ctx, "session.inference", agent.ModelRouteRef, prompt, messages, nil)
	if outcome.Class != provider.OutcomeCompleted && outcome.Class != provider.OutcomeLimit {
		return ioctx.Output{}, errors.New(outcome.Error)
	}
	answer := text(response["text"])
	if answer == "" {
		return ioctx.Output{}, errors.New("side/planning inference returned no text")
	}
	if request.Boundary != nil {
		if err := request.Boundary(ctx); err != nil {
			return ioctx.Output{}, err
		}
	}
	if request.Aside {
		return r.io.Emit(ctx, meta, ioctx.OutputRequest{EventID: stableID(request.SessionID, request.RunID, "aside"), OriginFrontend: request.OriginFrontend, SinkRefs: agent.SessionControls.PlanSinkRefs, Content: []byte(answer)})
	}
	assistant := message.Message{Role: message.RoleAssistant, Model: text(response["model"]), Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: answer}}}
	if usage, ok := tokenUsage(response["usage"]); ok {
		assistant.Usage = &usage
	}
	messages = append(messages, assistant)
	committed, err := r.session.Commit(ctx, sessionStage.Meta(meta), sessionStage.CommitRequest{SessionRef: agent.SessionRef, Messages: messages})
	if err != nil {
		return ioctx.Output{}, err
	}
	output, err := r.io.Emit(ctx, meta, ioctx.OutputRequest{EventID: stableID(request.SessionID, request.RunID, "plan"), OriginFrontend: request.OriginFrontend, SinkRefs: agent.SessionControls.PlanSinkRefs, Content: []byte(answer)})
	output.MessagesRef = committed.MessagesRef
	return output, err
}

// CompactSession uses the same YAML compaction policy and summarizer as the
// graph and makes the resulting history visible on subsequent turns.
func (r *Runtime) CompactSession(ctx context.Context, sessionID, runID, agentRef string) (memoryStage.CompactionResult, error) {
	agent, ok := r.doc.Spec.Agents[agentRef]
	if !ok {
		return memoryStage.CompactionResult{}, errors.New("undeclared agent")
	}
	ctx = context.WithValue(ctx, runKey{}, runContext{sessionID: sessionID, runID: runID, agentRef: agentRef})
	meta, _ := r.meta(ctx, "session.compact")
	events, err := event.Replay(r.eventPath)
	if err != nil {
		return memoryStage.CompactionResult{}, err
	}
	history, err := r.session.Load(ctx, sessionStage.Meta(meta), sessionStage.LoadRequest{SessionRef: agent.SessionRef, SessionID: sessionID, Events: events})
	if err != nil {
		return memoryStage.CompactionResult{}, err
	}
	result, err := r.memory.Compact(ctx, memoryStage.Meta(meta), memoryStage.CompactionRequest{MemoryRef: agent.MemoryRef, Messages: history.Messages})
	if err != nil {
		return result, err
	}
	return result, nil
}
