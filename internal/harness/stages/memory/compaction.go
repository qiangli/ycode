package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/spec"
)

type CompactionRequest struct {
	MemoryRef       string
	Messages        []message.Message
	PreviousSummary string
}

type CompactionResult struct {
	SchemaVersion   string            `json:"schema_version"`
	Triggered       bool              `json:"triggered"`
	Outcome         string            `json:"outcome"`
	InputRef        string            `json:"input_ref"`
	SummaryRef      string            `json:"summary_ref,omitempty"`
	MessagesRef     string            `json:"messages_ref"`
	InputTokens     int               `json:"input_tokens"`
	PreservedTokens int               `json:"preserved_tokens"`
	Summary         string            `json:"-"`
	Messages        []message.Message `json:"-"`
}

// Compact is invoked only by an explicit graph stage. It neither schedules
// itself nor chooses a route/fallback not present in the compiled memory.
func (e *Engine) Compact(ctx context.Context, meta Meta, request CompactionRequest) (CompactionResult, error) {
	if err := meta.validate(); err != nil {
		return CompactionResult{}, err
	}
	configured, err := e.memory(request.MemoryRef)
	if err != nil {
		return CompactionResult{}, err
	}
	policy := configured.Compaction
	if policy.PreserveRecentTokens <= 0 || policy.RouteRef == "" {
		return CompactionResult{}, errors.New("memory compaction: preservation budget and route are required")
	}
	if _, ok := e.routes[policy.RouteRef]; !ok {
		return CompactionResult{}, fmt.Errorf("memory compaction: undeclared route %q", policy.RouteRef)
	}
	if policy.OnFailure != "preserve-original" && policy.OnFailure != "fail" {
		return CompactionResult{}, fmt.Errorf("memory compaction: unsupported failure behavior %q", policy.OnFailure)
	}
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		return CompactionResult{}, err
	}
	inputRef, err := e.payloads.Put(encoded)
	if err != nil {
		return CompactionResult{}, err
	}
	total, err := e.tokens.CountMessages(request.Messages)
	if err != nil || total < 0 {
		return CompactionResult{}, errors.New("memory compaction: token measurement failed")
	}
	result := CompactionResult{SchemaVersion: SchemaVersion, InputRef: inputRef, InputTokens: total, Messages: cloneMessages(request.Messages)}
	if e.summary == nil {
		return e.compactionFailure(meta, request, policy, result, errors.New("summarizer is unavailable"))
	}
	cut, preservedTokens, err := e.preserveBoundary(request.Messages, policy.PreserveRecentTokens)
	if err != nil {
		return CompactionResult{}, err
	}
	if cut == 0 {
		result.Outcome = "nothing-to-compact"
		result.MessagesRef = inputRef
		_, err = e.append(meta, "memory.compaction.skipped", replayCompaction(result, request, policy))
		return result, err
	}
	summary, err := e.summary.Summarize(ctx, policy.RouteRef, cloneMessages(request.Messages[:cut]), request.PreviousSummary)
	if err != nil {
		return e.compactionFailure(meta, request, policy, result, err)
	}
	if summary == "" {
		return e.compactionFailure(meta, request, policy, result, errors.New("summarizer returned an empty summary"))
	}
	summaryRef, err := e.payloads.Put([]byte(summary))
	if err != nil {
		return CompactionResult{}, err
	}
	compacted := []message.Message{{Role: message.RoleSystem, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: summary}}}}
	compacted = append(compacted, cloneMessages(request.Messages[cut:])...)
	compactedBytes, err := json.Marshal(compacted)
	if err != nil {
		return CompactionResult{}, err
	}
	messagesRef, err := e.payloads.Put(compactedBytes)
	if err != nil {
		return CompactionResult{}, err
	}
	result.Triggered, result.Outcome, result.Summary, result.SummaryRef, result.Messages, result.MessagesRef, result.PreservedTokens = true, "compacted", summary, summaryRef, compacted, messagesRef, preservedTokens
	_, err = e.append(meta, "memory.compacted", replayCompaction(result, request, policy))
	return result, err
}

func (e *Engine) preserveBoundary(messages []message.Message, budget int) (int, int, error) {
	preserved := 0
	cut := len(messages)
	for cut > 0 {
		count, err := e.tokens.CountMessages(messages[cut-1 : cut])
		if err != nil || count < 0 {
			return 0, 0, errors.New("memory compaction: token measurement failed")
		}
		if preserved+count > budget {
			break
		}
		preserved += count
		cut--
	}
	return cut, preserved, nil
}

func (e *Engine) compactionFailure(meta Meta, request CompactionRequest, policy spec.CompactionPolicy, result CompactionResult, cause error) (CompactionResult, error) {
	result.Triggered = true
	result.MessagesRef = result.InputRef
	result.Messages = cloneMessages(request.Messages)
	result.Outcome = "failed"
	if policy.OnFailure == "preserve-original" {
		result.Outcome = "preserved-original"
		_, err := e.append(meta, "memory.compaction.preserved", map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "input_ref": result.InputRef, "messages_ref": result.MessagesRef, "input_tokens": result.InputTokens, "error": cause.Error()})
		return result, err
	}
	_, _ = e.append(meta, "memory.compaction.failed", map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "input_ref": result.InputRef, "input_tokens": result.InputTokens, "error": cause.Error()})
	return result, cause
}

func replayCompaction(result CompactionResult, request CompactionRequest, policy spec.CompactionPolicy) map[string]any {
	return map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "preserve_recent_tokens": policy.PreserveRecentTokens, "triggered": result.Triggered, "outcome": result.Outcome, "input_ref": result.InputRef, "summary_ref": result.SummaryRef, "messages_ref": result.MessagesRef, "input_tokens": result.InputTokens, "preserved_tokens": result.PreservedTokens}
}

func cloneMessages(messages []message.Message) []message.Message {
	data, _ := json.Marshal(messages)
	var result []message.Message
	_ = json.Unmarshal(data, &result)
	return result
}
