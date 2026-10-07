package turn

import (
	"context"
	"errors"

	"github.com/qiangli/ycode/internal/harness/event"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
)

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
