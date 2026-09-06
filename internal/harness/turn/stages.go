package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	api "github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	memexmemory "github.com/qiangli/ycode/pkg/memex/memory"
)

func (r *Runtime) lifecycle(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	ref, from, to := text(in.With["lifecycleRef"]), text(in.With["from"]), text(in.With["to"])
	configured, ok := r.doc.Spec.Lifecycles[ref]
	if !ok || to == "" {
		return fail(fmt.Errorf("lifecycle: invalid compiled transition for %q", ref))
	}
	allowed := false
	for _, transition := range configured.Transitions {
		if transition.To == to && (from == "" || transition.From == from) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fail(fmt.Errorf("lifecycle: transition %q -> %q is not compiled", from, to))
	}
	if err := r.append(ctx, in.StageID, "lifecycle.transitioned", map[string]any{"lifecycle_ref": ref, "from": from, "to": to}); err != nil {
		return fail(err)
	}
	return pipeline.Success(nil)
}

func (r *Runtime) input(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	value, ok := in.Inputs["request"].(ioctx.CanonicalInput)
	if !ok || value.SchemaVersion == "" {
		return fail(fmt.Errorf("input.normalize: expected admitted canonical input, got %T", in.Inputs["request"]))
	}
	return pipeline.Success(map[string]any{"input": value})
}

func (r *Runtime) context(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	result, err := r.io.LoadContext(ctx, meta, text(in.With["contextRef"]))
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"context": result})
}

func (r *Runtime) recall(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	input, ok := in.Inputs["query"].(ioctx.CanonicalInput)
	if !ok {
		return fail(fmt.Errorf("memory.recall: expected canonical input, got %T", in.Inputs["query"]))
	}
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	result, err := r.memory.Recall(ctx, memoryMeta(meta), text(in.With["memoryRef"]), requestText(input), run.agentRef)
	if err != nil {
		return fail(err)
	}
	messages := make([]ioctx.PromptMessage, 0, len(result.Items))
	for _, item := range result.Items {
		raw, err := r.payloads.Get(item.PayloadRef)
		if err != nil {
			return fail(err)
		}
		var memory memexmemory.Memory
		if err := json.Unmarshal(raw, &memory); err != nil {
			return fail(err)
		}
		messages = append(messages, ioctx.PromptMessage{Role: "system", Content: memory.Content, PayloadRef: item.PayloadRef})
	}
	return pipeline.Success(map[string]any{"items": messages})
}

func (r *Runtime) assemble(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	contextValue, ok := in.Inputs["context"].(ioctx.ContextSnapshot)
	if !ok {
		return fail(fmt.Errorf("prompt.assemble: invalid context %T", in.Inputs["context"]))
	}
	memoryValue, ok := in.Inputs["memory"].([]ioctx.PromptMessage)
	if !ok {
		return fail(fmt.Errorf("prompt.assemble: invalid memory %T", in.Inputs["memory"]))
	}
	inputValue, ok := in.Inputs["input"].(ioctx.CanonicalInput)
	if !ok {
		return fail(fmt.Errorf("prompt.assemble: invalid input %T", in.Inputs["input"]))
	}
	orderRaw := stringList(in.With["order"])
	order := make([]ioctx.PortName, len(orderRaw))
	for i := range orderRaw {
		order[i] = ioctx.PortName(orderRaw[i])
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	prompt, err := r.io.Assemble(ctx, meta, ioctx.PromptRequest{Order: order, Context: contextValue, Memory: memoryValue, Input: inputValue})
	if err != nil {
		return fail(err)
	}
	messages := make([]message.Message, 0, len(prompt.Messages))
	for _, item := range prompt.Messages {
		messages = append(messages, message.Message{Role: message.Role(item.Role), Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: item.Content}}})
	}
	return pipeline.Success(map[string]any{"state": map[string]any{
		"messages": messages, "providerSession": "", "response": map[string]any{},
		"finished": false, "remainingIterations": r.turnIterations(ctx),
	}})
}

func (r *Runtime) checkpoint(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	cp, err := r.events.SaveCheckpoint(checkpointPath(r.doc, run.sessionID, run.runID), run.sessionID, run.runID, in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	if err := r.append(ctx, in.StageID, "checkpoint.saved", map[string]any{"sequence": cp.Sequence, "reason": text(in.With["reason"])}); err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"checkpoint": cp})
}

func (r *Runtime) drain(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	items, err := r.queue.Drain(ctx, text(in.With["queueRef"]), stringList(in.With["classes"]))
	if err != nil {
		return fail(err)
	}
	ref, err := r.payload(items)
	if err != nil {
		return fail(err)
	}
	if err := r.append(ctx, in.StageID, "queue.drained", map[string]any{"queue_ref": text(in.With["queueRef"]), "payload_ref": ref, "count": len(items)}); err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"items": items})
}

func (r *Runtime) applyInput(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	items, ok := in.Inputs["items"].([]QueueItem)
	if !ok {
		return fail(fmt.Errorf("messages.apply-input: invalid items %T", in.Inputs["items"]))
	}
	for _, item := range items {
		if item.Text != "" {
			messages = append(messages, textMessage(message.RoleUser, item.Text))
		}
	}
	state["messages"] = messages
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) measure(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	messages, err := messagesFrom(in.Inputs["messages"])
	if err != nil {
		return fail(err)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	result, err := r.memory.Measure(ctx, memoryMeta(meta), messages)
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"tokens": result.Tokens})
}

func (r *Runtime) appendSource(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	source, ok := r.doc.Spec.Sources[text(in.With["sourceRef"])]
	if !ok {
		return fail(fmt.Errorf("messages.append-source: undeclared source %q", text(in.With["sourceRef"])))
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	messages = append(messages, textMessage(message.Role(text(in.With["role"])), source.Resolved))
	state["messages"] = messages
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) callModel(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	if text(in.With["tool"]) != provider.ToolName {
		return fail(fmt.Errorf("llm.call: compiled tool must be %q", provider.ToolName))
	}
	routeRef := text(in.With["routeRef"])
	route, ok := r.doc.Spec.Routes[routeRef]
	if !ok || len(route.Attempts) == 0 {
		return fail(fmt.Errorf("llm.call: invalid route %q", routeRef))
	}
	messages, err := messagesFrom(in.Inputs["messages"])
	if err != nil {
		return fail(err)
	}
	requestMessages, system := providerMessages(messages)
	var last provider.Outcome
	for attemptIndex, attempt := range route.Attempts {
		model, ok := r.doc.Spec.Models[attempt.ModelRef]
		if !ok {
			return fail(fmt.Errorf("llm.call: undeclared model %q", attempt.ModelRef))
		}
		adapter := r.providers[model.ProviderRef]
		if adapter == nil {
			return fail(fmt.Errorf("llm.call: unavailable provider %q", model.ProviderRef))
		}
		maxAttempts := attempt.TransportRetry.MaxAttempts
		if maxAttempts < 1 {
			return fail(fmt.Errorf("llm.call: route %q has invalid retry count", routeRef))
		}
		for transportAttempt := 1; transportAttempt <= maxAttempts; transportAttempt++ {
			maxTokens := route.Budget.MaxOutputTokens
			if maxTokens > model.Limits.MaxOutputTokens {
				maxTokens = model.Limits.MaxOutputTokens
			}
			request := provider.Request{Model: model.ID, System: system, Messages: requestMessages, MaxTokens: maxTokens, Stream: model.Capabilities.Streaming}
			requestRef, err := r.payload(request)
			if err != nil {
				return fail(err)
			}
			if err := r.append(ctx, in.StageID, "llm.requested", map[string]any{"route_ref": routeRef, "model_ref": attempt.ModelRef, "attempt": transportAttempt, "payload_ref": requestRef}); err != nil {
				return fail(err)
			}
			attemptCtx := ctx
			cancel := func() {}
			if attempt.TimeoutMS > 0 {
				attemptCtx, cancel = context.WithTimeout(ctx, time.Duration(attempt.TimeoutMS)*time.Millisecond)
			}
			response, outcome := collectProvider(attemptCtx, adapter.Send(attemptCtx, request))
			cancel()
			responseRef, err := r.payload(map[string]any{"response": response, "outcome": outcome})
			if err != nil {
				return fail(err)
			}
			if err := r.append(ctx, in.StageID, "llm.completed", map[string]any{"route_ref": routeRef, "model_ref": attempt.ModelRef, "attempt": transportAttempt, "payload_ref": responseRef, "outcome": outcome.Class}); err != nil {
				return fail(err)
			}
			last = outcome
			if outcome.Class == provider.OutcomeCompleted || outcome.Class == provider.OutcomeToolCall || outcome.Class == provider.OutcomeLimit {
				if outcome.Class == provider.OutcomeLimit {
					response["finished"] = true
				}
				return pipeline.Success(map[string]any{"response": response, "providerSession": in.Inputs["providerSession"]})
			}
			if transportAttempt == maxAttempts || !retryableProvider(outcome.Class, attempt.TransportRetry.RetryOn) {
				break
			}
			if err := waitBackoff(ctx, attempt.TransportRetry.Backoff, transportAttempt); err != nil {
				return fail(err)
			}
		}
		if attemptIndex+1 == len(route.Attempts) || !retryableProvider(last.Class, route.FallbackOn) {
			break
		}
	}
	return pipeline.Failure(string(last.Class), retryableProvider(last.Class, route.FallbackOn), errors.New(last.Error))
}

func (r *Runtime) normalize(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	response, err := object(in.Inputs["response"])
	if err != nil {
		return fail(err)
	}
	state["response"] = response
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) appendAssistant(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	response, err := object(in.Inputs["response"])
	if err != nil {
		return fail(err)
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	blocks := []message.ContentBlock{}
	if value := text(response["text"]); value != "" {
		blocks = append(blocks, message.ContentBlock{Type: message.ContentTypeText, Text: value})
	}
	for _, raw := range anyList(response["toolCalls"]) {
		call, err := object(raw)
		if err != nil {
			return fail(err)
		}
		input, _ := json.Marshal(call["input"])
		blocks = append(blocks, message.ContentBlock{Type: message.ContentTypeToolUse, ID: text(call["id"]), Name: text(call["name"]), Input: input})
	}
	if len(blocks) == 0 {
		return fail(errors.New("messages.append-assistant: provider response has no content"))
	}
	messages = append(messages, message.Message{Role: message.RoleAssistant, Content: blocks})
	state["messages"] = messages
	if remaining, ok := number(state["remainingIterations"]); ok {
		state["remainingIterations"] = remaining - 1
	}
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) finish(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	state["finished"] = true
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) writeMemory(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	messages, err := messagesFrom(in.Inputs["messages"])
	if err != nil {
		return fail(err)
	}
	ref := text(in.With["memoryRef"])
	items, err := r.materialize.Materialize(ctx, ref, messages)
	if err != nil {
		return fail(err)
	}
	if len(items) == 0 {
		if err := r.append(ctx, in.StageID, "memory.write.skipped", map[string]any{"memory_ref": ref, "reason": "materializer-empty"}); err != nil {
			return fail(err)
		}
		return pipeline.Success(nil)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	if _, err = r.memory.Write(ctx, memoryMeta(meta), ref, items); err != nil {
		return fail(err)
	}
	return pipeline.Success(nil)
}

func (r *Runtime) compact(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	result, err := r.memory.Compact(ctx, memoryMeta(meta), memoryStage.CompactionRequest{MemoryRef: text(in.With["memoryRef"]), Messages: messages})
	if err != nil {
		return fail(err)
	}
	state["messages"] = result.Messages
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) output(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	messages, err := messagesFrom(in.Inputs["messages"])
	if err != nil {
		return fail(err)
	}
	content := finalText(messages)
	if content == "" {
		return fail(errors.New("output.emit: no final assistant text"))
	}
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	result, err := r.io.Emit(ctx, meta, ioctx.OutputRequest{EventID: stableID(run.sessionID, run.runID, in.StageID, r.doc.ConfigDigest), OriginFrontend: run.originFrontend, SinkRefs: stringList(in.With["sinkRefs"]), Content: []byte(content)})
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"output": result})
}

func (r *Runtime) append(ctx context.Context, stage, kind string, data any) error {
	run, err := runFrom(ctx)
	if err != nil {
		return err
	}
	_, err = r.events.Append(event.Draft{SessionID: run.sessionID, RunID: run.runID, StageID: stage, Type: kind, ConfigDigest: r.doc.ConfigDigest, Data: data})
	return err
}

func (r *Runtime) payload(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return r.payloads.Put(raw)
}

func memoryMeta(meta ioctx.Meta) memoryStage.Meta {
	return memoryStage.Meta{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, ConfigDigest: meta.ConfigDigest}
}

func requestText(input ioctx.CanonicalInput) string {
	var value map[string]any
	if json.Unmarshal(input.Data, &value) == nil {
		if text, ok := value["request"].(string); ok {
			return text
		}
	}
	return string(input.Data)
}

func (r *Runtime) turnIterations(ctx context.Context) int {
	run, err := runFrom(ctx)
	if err != nil {
		return 0
	}
	agent := r.doc.Spec.Agents[run.agentRef]
	for _, node := range r.doc.Spec.Pipelines[agent.PipelineRef].Nodes {
		if node.Run.Repeat != nil {
			return node.Run.Repeat.MaxIterations
		}
	}
	return 0
}

func object(value any) (map[string]any, error) {
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("turn: expected object, got %T", value)
	}
	copy := make(map[string]any, len(result))
	for key, value := range result {
		copy[key] = value
	}
	return copy, nil
}

func messagesFrom(value any) ([]message.Message, error) {
	messages, ok := value.([]message.Message)
	if !ok {
		return nil, fmt.Errorf("turn: expected messages, got %T", value)
	}
	return append([]message.Message(nil), messages...), nil
}

func textMessage(role message.Role, value string) message.Message {
	return message.Message{Role: role, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: value}}}
}

func providerMessages(messages []message.Message) ([]api.Message, string) {
	var systemParts []string
	var out []api.Message
	for _, item := range messages {
		if item.Role == message.RoleSystem {
			for _, block := range item.Content {
				if block.Type == message.ContentTypeText {
					systemParts = append(systemParts, block.Text)
				}
			}
			continue
		}
		converted := api.Message{Role: api.MessageRole(item.Role)}
		for _, block := range item.Content {
			converted.Content = append(converted.Content, api.ContentBlock{Type: api.ContentType(block.Type), Text: block.Text, ID: block.ID, Name: block.Name, Input: block.Input, ToolUseID: block.ToolUseID, Content: block.Content, IsError: block.IsError})
		}
		out = append(out, converted)
	}
	return out, strings.Join(systemParts, "\n\n")
}

func collectProvider(ctx context.Context, stream <-chan provider.Event) (map[string]any, provider.Outcome) {
	response := map[string]any{"text": "", "thinking": "", "toolCalls": []any{}, "hasToolCalls": false, "finished": false}
	var outcome provider.Outcome
	for {
		select {
		case <-ctx.Done():
			return response, provider.Outcome{Class: provider.OutcomeCanceled, Error: ctx.Err().Error()}
		case value, ok := <-stream:
			if !ok {
				return response, outcome
			}
			switch value.Type {
			case provider.EventTextDelta:
				response["text"] = text(response["text"]) + value.Text
			case provider.EventThinkingDelta:
				response["thinking"] = text(response["thinking"]) + value.Text
			case provider.EventToolCall:
				if value.ToolCall != nil {
					var input any
					_ = json.Unmarshal(value.ToolCall.Input, &input)
					response["toolCalls"] = append(anyList(response["toolCalls"]), map[string]any{"id": value.ToolCall.ID, "name": value.ToolCall.Name, "input": input})
					response["hasToolCalls"] = true
				}
			case provider.EventUsage:
				if value.Usage != nil {
					response["usage"] = *value.Usage
				}
			case provider.EventOutcome:
				if value.Outcome != nil {
					outcome = *value.Outcome
					response["finished"] = outcome.Class == provider.OutcomeCompleted
				}
			}
		}
	}
}

func retryableProvider(class provider.OutcomeClass, configured []string) bool {
	want := string(class)
	aliases := map[string]string{"provider_error": "transport", "deadline": "timeout", "limit": "rate-limit"}
	for _, value := range configured {
		if value == want || value == aliases[want] {
			return true
		}
	}
	return false
}

func waitBackoff(ctx context.Context, value spec.Backoff, attempt int) error {
	delay := float64(value.InitialMS)
	for i := 1; i < attempt; i++ {
		delay *= value.Multiplier
	}
	if delay > float64(value.MaxMS) {
		delay = float64(value.MaxMS)
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func finalText(messages []message.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != message.RoleAssistant {
			continue
		}
		var parts []string
		for _, block := range messages[i].Content {
			if block.Type == message.ContentTypeText && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		if len(parts) != 0 {
			return strings.Join(parts, "")
		}
	}
	return ""
}

func anyList(value any) []any { result, _ := value.([]any); return result }

func number(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func stableID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
