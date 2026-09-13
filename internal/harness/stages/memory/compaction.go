package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	if policy.PromptSourceRef == "" || policy.UpdatePromptSourceRef == "" {
		return CompactionResult{}, errors.New("memory compaction: prompt sources are required")
	}
	if _, ok := e.routes[policy.RouteRef]; !ok {
		return CompactionResult{}, fmt.Errorf("memory compaction: undeclared route %q", policy.RouteRef)
	}
	if _, ok := e.sources[policy.PromptSourceRef]; !ok {
		return CompactionResult{}, fmt.Errorf("memory compaction: undeclared prompt source %q", policy.PromptSourceRef)
	}
	if _, ok := e.sources[policy.UpdatePromptSourceRef]; !ok {
		return CompactionResult{}, fmt.Errorf("memory compaction: undeclared update prompt source %q", policy.UpdatePromptSourceRef)
	}
	if policy.OnFailure != "preserve-original" && policy.OnFailure != "fallback-deterministic" && policy.OnFailure != "fail" {
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
	prompt := e.compactionPrompt(policy, request.PreviousSummary)
	summaryInput := e.summaryMessages(request.Messages[:cut], request.PreviousSummary)
	summary, err := e.summary.Summarize(ctx, policy.RouteRef, prompt, summaryInput, request.PreviousSummary)
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
	pinned, pinnedTokens, err := e.preserveUserMessages(request.Messages[:cut], policy.PreserveUserMessagesTokens)
	if err != nil {
		return CompactionResult{}, err
	}
	preservedTokens += pinnedTokens
	compacted := pinned
	compacted = append(compacted, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: e.handoffSummaryText(prompt, summary)}}})
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
	for cut > 0 && cut < len(messages) && hasToolUse(messages[cut-1]) && hasToolResult(messages[cut]) {
		count, err := e.tokens.CountMessages(messages[cut-1 : cut])
		if err != nil || count < 0 {
			return 0, 0, errors.New("memory compaction: token measurement failed")
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
	if policy.OnFailure == "fallback-deterministic" {
		prompt := e.compactionPrompt(policy, request.PreviousSummary)
		summary := deterministicSummary(request.Messages)
		compacted := []message.Message{{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: e.handoffSummaryText(prompt, summary)}}}}
		compacted = append(compacted, cloneMessages(request.Messages)...)
		encoded, err := json.Marshal(compacted)
		if err != nil {
			return CompactionResult{}, err
		}
		ref, err := e.payloads.Put(encoded)
		if err != nil {
			return CompactionResult{}, err
		}
		result.Outcome = "fallback-deterministic"
		result.Summary = summary
		result.Messages = compacted
		result.MessagesRef = ref
		_, err = e.append(meta, "memory.compaction.fallback_deterministic", map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "input_ref": result.InputRef, "messages_ref": result.MessagesRef, "input_tokens": result.InputTokens, "error": cause.Error()})
		return result, err
	}
	if policy.OnFailure == "preserve-original" {
		result.Outcome = "preserved-original"
		_, err := e.append(meta, "memory.compaction.preserved", map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "input_ref": result.InputRef, "messages_ref": result.MessagesRef, "input_tokens": result.InputTokens, "error": cause.Error()})
		return result, err
	}
	_, _ = e.append(meta, "memory.compaction.failed", map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "input_ref": result.InputRef, "input_tokens": result.InputTokens, "error": cause.Error()})
	return result, cause
}

func replayCompaction(result CompactionResult, request CompactionRequest, policy spec.CompactionPolicy) map[string]any {
	compactions := 0
	if result.Triggered && result.Outcome == "compacted" {
		compactions = 1
	}
	return map[string]any{"schema_version": SchemaVersion, "memory_ref": request.MemoryRef, "route_ref": policy.RouteRef, "on_failure": policy.OnFailure, "preserve_recent_tokens": policy.PreserveRecentTokens, "preserve_user_messages_tokens": policy.PreserveUserMessagesTokens, "triggered": result.Triggered, "outcome": result.Outcome, "input_ref": result.InputRef, "summary_ref": result.SummaryRef, "messages_ref": result.MessagesRef, "input_tokens": result.InputTokens, "preserved_tokens": result.PreservedTokens, "compactions": compactions}
}

func (e *Engine) compactionPrompt(policy spec.CompactionPolicy, previous string) string {
	ref := policy.PromptSourceRef
	if strings.TrimSpace(previous) != "" {
		ref = policy.UpdatePromptSourceRef
	}
	return e.sources[ref].Resolved
}

func (e *Engine) summaryMessages(messages []message.Message, previous string) []message.Message {
	var parts []string
	if strings.TrimSpace(previous) != "" {
		parts = append(parts, "PreviousSummary:\n"+previous)
	}
	encoded, _ := json.Marshal(messages)
	parts = append(parts, "Turns:\n"+string(encoded))
	return []message.Message{{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: strings.Join(parts, "\n\n")}}}}
}

func (e *Engine) handoffSummaryText(prompt, summary string) string {
	prefix := firstLine(prompt)
	if prefix == "" {
		prefix = "[compaction-summary]"
	}
	if !strings.Contains(prefix, "compaction-summary") {
		prefix = "[compaction-summary] " + prefix
	}
	return prefix + "\n" + summary
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func (e *Engine) preserveUserMessages(messages []message.Message, budget int) ([]message.Message, int, error) {
	if budget <= 0 {
		return nil, 0, nil
	}
	var reversed []message.Message
	used := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != message.RoleUser {
			continue
		}
		count, err := e.tokens.CountMessages(messages[i : i+1])
		if err != nil || count < 0 {
			return nil, 0, errors.New("memory compaction: token measurement failed")
		}
		if used+count > budget {
			break
		}
		used += count
		reversed = append(reversed, cloneMessages(messages[i : i+1])[0])
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, used, nil
}

func hasToolUse(item message.Message) bool {
	for _, block := range item.Content {
		if block.Type == message.ContentTypeToolUse {
			return true
		}
	}
	return false
}

func hasToolResult(item message.Message) bool {
	for _, block := range item.Content {
		if block.Type == message.ContentTypeToolResult {
			return true
		}
	}
	return false
}

func deterministicSummary(messages []message.Message) string {
	var lines []string
	lines = append(lines, "Deterministic excerpt summary:")
	for _, item := range messages {
		text := messageText(item)
		if text == "" {
			continue
		}
		if len(text) > 240 {
			text = text[:240]
		}
		lines = append(lines, "- "+string(item.Role)+": "+text)
		if len(lines) == 13 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func messageText(item message.Message) string {
	var parts []string
	for _, block := range item.Content {
		switch block.Type {
		case message.ContentTypeText:
			parts = append(parts, block.Text)
		case message.ContentTypeToolUse:
			parts = append(parts, "tool_use "+block.Name)
		case message.ContentTypeToolResult:
			parts = append(parts, "tool_result "+block.ToolUseID+": "+block.Content)
		}
	}
	return strings.Join(parts, " ")
}

func cloneMessages(messages []message.Message) []message.Message {
	data, _ := json.Marshal(messages)
	var result []message.Message
	_ = json.Unmarshal(data, &result)
	return result
}
