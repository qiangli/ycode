package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
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
	sessionStage "github.com/qiangli/ycode/internal/harness/stages/session"
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
	// The optional text port projects the canonical request into one scalar so
	// downstream nodes (a bashy.run script template, for example) can bind the
	// request text without decoding the canonical JSON themselves.
	return pipeline.Success(map[string]any{"input": value, "text": requestText(value)})
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

func (r *Runtime) loadSession(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	events, err := r.history.Replay()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	result, err := r.session.Load(ctx, sessionStage.Meta(meta), sessionStage.LoadRequest{SessionRef: text(in.With["sessionRef"]), SessionID: run.sessionID, Events: events})
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"history": result.Messages})
}

func (r *Runtime) assemble(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	contextValue := ioctx.ContextSnapshot{}
	if raw, exists := in.Inputs["context"]; exists {
		var ok bool
		contextValue, ok = raw.(ioctx.ContextSnapshot)
		if !ok {
			return fail(fmt.Errorf("prompt.assemble: invalid context %T", raw))
		}
	}
	var knowledgeValue []ioctx.PromptMessage
	if raw, exists := in.Inputs["knowledge"]; exists {
		var err error
		knowledgeValue, err = memoryStage.KnowledgeMessages(raw)
		if err != nil {
			return fail(fmt.Errorf("prompt.assemble: invalid knowledge port: %w", err))
		}
	}
	orderRaw := stringList(in.With["order"])
	order := make([]ioctx.PortName, len(orderRaw))
	for i := range orderRaw {
		order[i] = ioctx.PortName(orderRaw[i])
	}
	var historyMessages []message.Message
	if _, wantsHistory := containsPort(order, ioctx.PortHistory); wantsHistory {
		var ok bool
		historyMessages, ok = in.Inputs["history"].([]message.Message)
		if !ok {
			return fail(fmt.Errorf("prompt.assemble: invalid history %T", in.Inputs["history"]))
		}
	}
	inputValue, ok := in.Inputs["input"].(ioctx.CanonicalInput)
	if !ok {
		return fail(fmt.Errorf("prompt.assemble: invalid input %T", in.Inputs["input"]))
	}
	meta, err := r.meta(ctx, in.StageID)
	if err != nil {
		return fail(err)
	}
	historyPrompt := make([]ioctx.PromptMessage, 0, len(historyMessages))
	for _, item := range historyMessages {
		ref, err := r.payload(item)
		if err != nil {
			return fail(err)
		}
		historyPrompt = append(historyPrompt, ioctx.PromptMessage{Role: string(item.Role), Content: messageText(item), PayloadRef: ref})
	}
	prompt, err := r.io.Assemble(ctx, meta, ioctx.PromptRequest{Order: order, Context: contextValue, Knowledge: knowledgeValue, History: historyPrompt, Input: inputValue})
	if err != nil {
		return fail(err)
	}
	messages := make([]message.Message, 0, len(prompt.Messages)+len(historyMessages))
	for _, port := range order {
		switch port {
		case ioctx.PortContext:
			for _, fragment := range contextValue.Fragments {
				messages = append(messages, message.Message{Role: message.Role(fragment.Role), Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: fragment.Content, CacheBreak: fragment.BreakAfter}}})
			}
		case ioctx.PortKnowledge:
			for _, item := range knowledgeValue {
				messages = append(messages, message.Message{Role: message.Role(item.Role), Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: item.Content}}})
			}
		case ioctx.PortHistory:
			messages = append(messages, cloneMessages(historyMessages)...)
		case ioctx.PortInput:
			messages = append(messages, message.Message{Role: message.RoleUser, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: string(inputValue.Data)}}})
		}
	}
	return pipeline.Success(map[string]any{"state": map[string]any{
		"messages": messages, "providerSession": "", "response": map[string]any{},
		"finished": false, "remainingIterations": r.turnIterations(ctx),
	}})
}

func (r *Runtime) commitSession(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	if run, err := runFrom(ctx); err == nil && run.aside {
		return pipeline.Success(map[string]any{"messagesRef": ""})
	}
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
	compactions, _ := number(state["compactions"])
	result, err := r.session.Commit(ctx, sessionStage.Meta(meta), sessionStage.CommitRequest{SessionRef: text(in.With["sessionRef"]), Messages: messages, Compactions: compactions})
	if err != nil {
		return fail(err)
	}
	state["messagesRef"] = result.MessagesRef
	return pipeline.Success(map[string]any{"messagesRef": result.MessagesRef})
}

func containsPort(order []ioctx.PortName, target ioctx.PortName) (int, bool) {
	for i, value := range order {
		if value == target {
			return i, true
		}
	}
	return -1, false
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
	run, err := runFrom(ctx)
	if err != nil {
		return fail(err)
	}
	if run.aside {
		return pipeline.Success(map[string]any{"items": []QueueItem{}})
	}
	items, err := r.queue.Drain(ctx, run.sessionID, text(in.With["queueRef"]), stringList(in.With["classes"]))
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
	margin, ok := floatValue(in.With["safetyMargin"])
	if !ok {
		margin = 1
	}
	routeRef := text(in.With["routeRef"])
	run, _ := runFrom(ctx)
	result, err := r.memory.Measure(ctx, memoryMeta(meta), memoryStage.MeasureRequest{MemoryRef: text(in.With["memoryRef"]), RouteRef: routeRef, ModelRef: sessionModelRef(run, r.doc, routeRef), SafetyMargin: margin, Messages: messages})
	if err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"tokens": result.Tokens, "contextBudget": result.ContextBudget, "truncateBudget": result.TruncateBudget, "measured": result.Measured})
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
	routeRef := text(in.With["routeRef"])
	route, ok := r.doc.Spec.Routes[routeRef]
	if !ok || len(route.Attempts) == 0 {
		return fail(fmt.Errorf("llm.call: invalid route %q", routeRef))
	}
	messages, err := messagesFrom(in.Inputs["messages"])
	if err != nil {
		return fail(err)
	}
	// Apply the selected mode only to conversational inference. Compaction
	// keeps its own declared summary prompt and route contract.
	_, system := providerMessages(messages)
	if run, err := runFrom(ctx); err == nil && run.plan {
		controls := r.doc.Spec.Agents[run.agentRef].SessionControls
		instruction := controls.PlanPrompt
		if run.aside {
			instruction = controls.BtwPrompt
		}
		// A mode instruction is per-turn, so it is appended after every
		// declared segment: nothing unstable may precede a cached prefix.
		system = append(system, api.SystemBlock{Type: "text", Text: instruction})
	}
	response, outcome := r.routeProvider(ctx, in.StageID, routeRef, system, messages, in.Inputs["providerSession"])
	if outcome.Class == provider.OutcomeCompleted || outcome.Class == provider.OutcomeToolCall || outcome.Class == provider.OutcomeLimit {
		return pipeline.Success(map[string]any{"response": response, "providerSession": in.Inputs["providerSession"]})
	}
	return pipeline.Failure(string(outcome.Class), retryableProvider(outcome.Class, route.FallbackOn), errors.New(outcome.Error))
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
	response, err = normalizeProviderResponse(response, in.With)
	if err != nil {
		return fail(err)
	}
	state["response"] = response
	return pipeline.Success(map[string]any{"state": state})
}

// normalizeProviderResponse applies the compiled messages.normalize-provider-response
// policy to one provider turn, before any call is dispatched:
//
//   - deterministicCallIds assigns every call an id derived from its own name
//     and input, so two calls the model (or a flaky provider echo) emitted
//     with the exact same script resolve to the same id instead of two
//     distinct ones racing the Bashy tool.
//   - duplicateCalls "drop-identical" then collapses same-signature calls to
//     their first occurrence, so the compiled execute-tool-calls forEach
//     dispatches an identical bashy call once per step, not twice.
//   - malformedToolResult "repair-explicitly" gives a call with no input an
//     explicit empty object instead of a nil that would otherwise reach the
//     dispatcher as a JSON "null".
func normalizeProviderResponse(response map[string]any, with map[string]any) (map[string]any, error) {
	calls := anyList(response["toolCalls"])
	if len(calls) == 0 {
		return response, nil
	}
	deterministic, _ := with["deterministicCallIds"].(bool)
	dropIdentical := text(with["duplicateCalls"]) == "drop-identical"
	repairMalformed := text(with["malformedToolResult"]) == "repair-explicitly"
	seen := make(map[string]bool, len(calls))
	kept := make([]any, 0, len(calls))
	for _, raw := range calls {
		call, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("messages.normalize-provider-response: invalid tool call %T", raw)
		}
		input := call["input"]
		if repairMalformed && input == nil {
			input = map[string]any{}
			call["input"] = input
		}
		encodedInput, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("messages.normalize-provider-response: encode tool call input: %w", err)
		}
		signature := text(call["name"]) + "\x00" + string(encodedInput)
		if deterministic {
			call["id"] = stableID("tool-call", signature)
		}
		if dropIdentical {
			if seen[signature] {
				continue
			}
			seen[signature] = true
		}
		kept = append(kept, call)
	}
	response["toolCalls"] = kept
	return response, nil
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
	assistant := message.Message{Role: message.RoleAssistant, Content: blocks, Model: text(response["model"])}
	if usage, ok := tokenUsage(response["usage"]); ok {
		assistant.Usage = &usage
	}
	messages = append(messages, assistant)
	state["messages"] = messages
	if remaining, ok := number(state["remainingIterations"]); ok {
		state["remainingIterations"] = remaining - 1
	}
	return pipeline.Success(map[string]any{"state": state})
}

// finish ends the agent loop, unless a headless one-shot turn with no human
// to answer just ended on a question or menu instead of acting (docs/todo/
// 1d4096a6c973): with.maxContinuations and with.sourceRef opt a pipeline into
// a bounded nudge instead — an interactive finish-step that declares neither
// is unaffected.
//
// with.onTextOnly names a second source on its own bound (Sprint 412 story
// 7dcd4d1c follow-up): a headless one-shot turn that already executed at
// least one tool call but whose final reply carries no tool call is a format
// error — the turn appends that source's text and continues instead of
// ending with nothing done. The text-only nudge must not burn the
// question/menu budget on a finished run (a summary without DONE re-verifying
// until maxContinuations ran out), so with.maxTextOnlyContinuations (default
// 1) bounds it via its own state counter, textOnlyContinuations, and a
// second text-only reply after the nudge finishes the turn. A text-only
// reply before any tool call ran still finishes.
//
// A reply that declares completion — DONE as a standalone word, which the
// nudge text itself instructs — finishes before either nudge is considered,
// on both paths: a finished run says so once and is believed.
func (r *Runtime) finish(ctx context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	maxContinuations, _ := number(in.With["maxContinuations"])
	sourceRef := text(in.With["sourceRef"])
	onTextOnly := text(in.With["onTextOnly"])
	maxTextOnly, ok := number(in.With["maxTextOnlyContinuations"])
	if !ok {
		maxTextOnly = 1
	}
	questionPath := maxContinuations > 0 && sourceRef != ""
	textOnlyPath := maxTextOnly > 0 && onTextOnly != ""
	if (questionPath || textOnlyPath) && !declaresCompletion(state) {
		if questionPath {
			if count, _ := number(state["continuations"]); count < maxContinuations {
				if r.wantsHeadlessContinuation(ctx, state) {
					return r.continueLoop(ctx, in.StageID, state, sourceRef, "continuations", count, maxContinuations)
				}
			}
		}
		if textOnlyPath {
			if count, _ := number(state["textOnlyContinuations"]); count < maxTextOnly {
				if r.wantsTextOnlyContinuation(ctx, state) {
					return r.continueLoop(ctx, in.StageID, state, onTextOnly, "textOnlyContinuations", count, maxTextOnly)
				}
			}
		}
	}
	state["finished"] = true
	return pipeline.Success(map[string]any{"state": state})
}

// continueLoop appends a continuation source as a user message and returns
// the unfinished state, spending one continuation of the named counter's
// budget.
func (r *Runtime) continueLoop(ctx context.Context, stageID string, state map[string]any, sourceRef, counterKey string, count, max int) pipeline.Outcome {
	source, ok := r.doc.Spec.Sources[sourceRef]
	if !ok {
		return fail(fmt.Errorf("loop.finish: undeclared source %q", sourceRef))
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	state["messages"] = append(messages, textMessage(message.RoleUser, source.Resolved))
	state[counterKey] = count + 1
	if err := r.append(ctx, stageID, "loop.continued", map[string]any{"count": count + 1, "max": max}); err != nil {
		return fail(err)
	}
	return pipeline.Success(map[string]any{"state": state})
}

// headlessOneShot reports a turn with no human available to answer running
// under a one-shot frontend: nudging is safe only there, never in an
// interactive session where a text-only reply may be the real answer.
func (r *Runtime) headlessOneShot(ctx context.Context) bool {
	run, err := runFrom(ctx)
	if err != nil || run.humanAvailable {
		return false
	}
	return r.doc.Spec.Frontends[run.originFrontend].Kind == "one-shot"
}

// wantsHeadlessContinuation reports a turn with no human available to answer
// (a one-shot frontend run without HITL) whose last assistant message reads
// as a question or menu rather than a completed action — the live failure
// this nudge exists for: the model analyzes correctly, then ends the turn
// asking which option to take instead of taking one.
func (r *Runtime) wantsHeadlessContinuation(ctx context.Context, state map[string]any) bool {
	if !r.headlessOneShot(ctx) {
		return false
	}
	reply, _, ok := finalAssistantReply(state)
	if !ok {
		return false
	}
	return readsAsQuestionOrMenu(reply)
}

// menuOptionLine matches one numbered or lettered option line ("1.", "2)",
// "A)", "b." or "Option A"). Plain bullet lines ("- ", "* ") are excluded on
// purpose (Sprint 412 story 7dcd4d1c follow-up 2): declarative final
// summaries are routinely bulleted, and counting those as a menu nudged
// every finished instance of the Sprint 412 r3 slice twice.
var menuOptionLine = regexp.MustCompile(`(?m)^\s*(?:(?:\d{1,2}|[A-Za-z])[.)]\s+\S|[Oo]ption\s+[A-Za-z0-9]+\b)`)

// askingCue matches the phrasing that turns a list into a request for a
// decision rather than a report of one.
var askingCue = regexp.MustCompile(`(?i)\b(which|would you like|should i|do you want|prefer|choose)\b`)

// blankLine splits paragraphs.
var blankLine = regexp.MustCompile(`\n[ \t]*\n`)

// readsAsQuestionOrMenu reports a reply that hands the decision back to a
// human: it either ends on a question mark, or lays out two or more numbered
// or lettered options and asks for a pick in its closing paragraph. A
// declarative summary — bulleted or not — is neither.
func readsAsQuestionOrMenu(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.HasSuffix(trimmed, "?") {
		return true
	}
	if len(menuOptionLine.FindAllString(trimmed, 2)) < 2 {
		return false
	}
	return askingCue.MatchString(lastParagraph(trimmed))
}

// lastParagraph returns the reply's closing paragraph: the text after its
// final blank line, or the whole reply when it has none.
func lastParagraph(value string) string {
	parts := blankLine.Split(value, -1)
	return strings.TrimSpace(parts[len(parts)-1])
}

// doneWord matches the completion marker as a standalone, case-sensitive
// word, so "DONE", "DONE:" and a bare "DONE" line all declare completion
// while "ABANDONED" (which contains those letters) and a casual lowercase
// "done" do not.
var doneWord = regexp.MustCompile(`\bDONE\b`)

// declaresCompletion reports a final text-only assistant reply that declares
// the work finished. Both nudges are skipped for such a reply.
func declaresCompletion(state map[string]any) bool {
	reply, _, ok := finalAssistantReply(state)
	return ok && doneWord.MatchString(reply)
}

// finalAssistantReply returns the concatenated text of the turn's last
// message together with the turn's messages, reporting false unless that
// message is a text-only assistant reply — a trailing tool call is the loop
// running normally, never something to nudge or to finish on.
func finalAssistantReply(state map[string]any) (string, []message.Message, bool) {
	messages, err := messagesFrom(state["messages"])
	if err != nil || len(messages) == 0 {
		return "", nil, false
	}
	last := messages[len(messages)-1]
	if last.Role != message.RoleAssistant {
		return "", nil, false
	}
	var parts []string
	for _, block := range last.Content {
		if block.Type == message.ContentTypeToolUse {
			return "", nil, false
		}
		if block.Type == message.ContentTypeText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, ""), messages, true
}

// wantsTextOnlyContinuation reports a headless one-shot turn that already
// executed at least one tool call but whose last assistant message carries
// no tool call: the task is mid-flight yet nothing will run, so ending here
// silently drops the work (Sprint 322 django-15280 ended with no diff). A
// reply that declares completion never reaches here — finish checks that
// first. A text-only reply before any tool call ran also finishes, so pure
// Q&A turns never loop.
func (r *Runtime) wantsTextOnlyContinuation(ctx context.Context, state map[string]any) bool {
	if !r.headlessOneShot(ctx) {
		return false
	}
	reply, messages, ok := finalAssistantReply(state)
	if !ok || strings.TrimSpace(reply) == "" {
		return false
	}
	return turnExecutedToolCall(messages)
}

// turnExecutedToolCall reports whether any message in the turn executed a
// tool call: an assistant tool_use block or its tool_result.
func turnExecutedToolCall(messages []message.Message) bool {
	for _, item := range messages {
		for _, block := range item.Content {
			if block.Type == message.ContentTypeToolUse || block.Type == message.ContentTypeToolResult {
				return true
			}
		}
	}
	return false
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
	result, err := r.memory.Compact(ctx, memoryMeta(meta), memoryStage.CompactionRequest{MemoryRef: text(in.With["memoryRef"]), Messages: messages, PreviousSummary: text(state["summary"])})
	if err != nil {
		return fail(err)
	}
	state["messages"] = result.Messages
	if result.Summary != "" {
		state["summary"] = result.Summary
	}
	if result.Outcome == "compacted" || result.Outcome == "fallback-deterministic" {
		count, _ := number(state["compactions"])
		state["compactions"] = count + 1
	}
	return pipeline.Success(map[string]any{"state": state})
}

func (r *Runtime) clearToolResults(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
	state, err := object(in.Inputs["state"])
	if err != nil {
		return fail(err)
	}
	messages, err := messagesFrom(state["messages"])
	if err != nil {
		return fail(err)
	}
	olderThanTurns, ok := number(in.With["olderThanTurns"])
	if !ok || olderThanTurns < 0 {
		return fail(errors.New("messages.clear-tool-results: olderThanTurns must be non-negative"))
	}
	placeholder := text(in.With["placeholder"])
	if placeholder == "" {
		return fail(errors.New("messages.clear-tool-results: placeholder is required"))
	}
	userSeen := 0
	for i := len(messages) - 1; i >= 0; i-- {
		// A turn boundary is a genuine user message, not a tool_result
		// synthesized under the same RoleUser: one real turn's agent loop
		// can carry dozens of those, and counting each as its own turn
		// cleared almost everything after only a handful of tool calls.
		if messages[i].Role == message.RoleUser && !isToolResultMessage(messages[i]) {
			userSeen++
		}
		if userSeen <= olderThanTurns {
			continue
		}
		for j := range messages[i].Content {
			if messages[i].Content[j].Type == message.ContentTypeToolResult {
				messages[i].Content[j].Content = placeholder
				messages[i].Content[j].IsError = false
			}
		}
	}
	state["messages"] = messages
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

func cloneMessages(messages []message.Message) []message.Message {
	raw, _ := json.Marshal(messages)
	var result []message.Message
	_ = json.Unmarshal(raw, &result)
	return result
}

func textMessage(role message.Role, value string) message.Message {
	return message.Message{Role: role, Content: []message.ContentBlock{{Type: message.ContentTypeText, Text: value}}}
}

// isToolResultMessage reports a message synthesized to carry one or more
// tool_result blocks back to the model: it has RoleUser like a genuine user
// turn, but is not one.
func isToolResultMessage(item message.Message) bool {
	if item.Role != message.RoleUser || len(item.Content) == 0 {
		return false
	}
	for _, block := range item.Content {
		if block.Type != message.ContentTypeToolResult {
			return false
		}
	}
	return true
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

// providerMessages splits the neutral message list into the provider's
// conversation messages and its system prompt. The system prompt is segmented:
// a text block that ends a declared cache segment (a context fragment with
// cache.breakAfter) closes the current block, so a protocol that places cache
// breakpoints can mark exactly the prefixes the author declared. Declared
// order is preserved, which keeps the stable segments first and per-turn text
// last; systemText flattens the same segments back to one string.
func providerMessages(messages []message.Message) ([]api.Message, []api.SystemBlock) {
	var systemBlocks []api.SystemBlock
	var systemParts []string
	flushSystem := func(cacheBreak bool) {
		// An empty segment is not a cache boundary and must not become a
		// block: Anthropic rejects an empty system block outright.
		if len(systemParts) == 0 {
			return
		}
		systemBlocks = append(systemBlocks, api.SystemBlock{Type: "text", Text: strings.Join(systemParts, "\n\n"), CacheBreak: cacheBreak})
		systemParts = nil
	}
	var out []api.Message
	for _, item := range messages {
		if item.Role == message.RoleSystem {
			for _, block := range item.Content {
				if block.Type != message.ContentTypeText {
					continue
				}
				if block.Text != "" {
					systemParts = append(systemParts, block.Text)
				}
				if block.CacheBreak {
					flushSystem(true)
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
	flushSystem(false)
	return out, systemBlocks
}

// systemText flattens segmented system blocks to the single-string form,
// reproducing the text exactly as it read before segmentation.
func systemText(blocks []api.SystemBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// deltaFlushBytes bounds how much provider text a delta event carries before
// it is journaled: a frontend sees text a line (or this many bytes) at a time
// without one journal append per provider token.
const deltaFlushBytes = 80

// deltaSink receives a provider's text as it arrives, coalesced; channel is
// "text" or "thinking". Nil means no live observer. An error stops the
// collection: a delta the observer could not record is a failed attempt, not
// a silently dropped line.
type deltaSink func(channel, text string) error

// collectProvider folds a provider stream into one response. Deltas are also
// forwarded to onDelta (when set) as they arrive, so a frontend can render the
// answer before the turn completes; the folded response stays canonical.
func collectProvider(ctx context.Context, stream <-chan provider.Event, onDelta ...deltaSink) (map[string]any, provider.Outcome) {
	response := map[string]any{"text": "", "thinking": "", "toolCalls": []any{}, "hasToolCalls": false, "finished": false}
	var outcome provider.Outcome
	var sink deltaSink
	if len(onDelta) > 0 {
		sink = onDelta[0]
	}
	pending := map[string]*strings.Builder{"text": {}, "thinking": {}}
	var sinkErr error
	flush := func(channel string) {
		if b := pending[channel]; sink != nil && sinkErr == nil && b.Len() > 0 {
			sinkErr = sink(channel, b.String())
			b.Reset()
		}
	}
	forward := func(channel, chunk string) {
		if sink == nil || chunk == "" {
			return
		}
		b := pending[channel]
		b.WriteString(chunk)
		if b.Len() >= deltaFlushBytes || strings.Contains(chunk, "\n") {
			flush(channel)
		}
	}
	failed := func() provider.Outcome {
		return provider.Outcome{Class: provider.OutcomeProtocolError, Error: sinkErr.Error()}
	}
	for {
		if sinkErr != nil {
			return response, failed()
		}
		select {
		case <-ctx.Done():
			flush("thinking")
			flush("text")
			return response, provider.Outcome{Class: provider.OutcomeCanceled, Error: ctx.Err().Error()}
		case value, ok := <-stream:
			if !ok {
				flush("thinking")
				flush("text")
				if sinkErr != nil {
					return response, failed()
				}
				return response, outcome
			}
			switch value.Type {
			case provider.EventTextDelta:
				response["text"] = text(response["text"]) + value.Text
				forward("text", value.Text)
			case provider.EventThinkingDelta:
				response["thinking"] = text(response["thinking"]) + value.Text
				forward("thinking", value.Text)
			case provider.EventToolCall:
				if value.ToolCall != nil {
					var input any
					_ = json.Unmarshal(value.ToolCall.Input, &input)
					response["toolCalls"] = append(anyList(response["toolCalls"]), map[string]any{"id": value.ToolCall.ID, "name": value.ToolCall.Name, "input": input})
					response["hasToolCalls"] = true
				}
			case provider.EventUsage:
				if value.Usage != nil {
					// One response's usage can arrive split across events
					// (input tokens, then output tokens): merge, never replace.
					prior, _ := response["usage"].(provider.Usage)
					response["usage"] = mergeUsage(prior, *value.Usage)
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

// mergeUsage folds a later usage event into the ones before it: a non-zero
// field updates the value (a cumulative restatement), a zero field keeps it.
func mergeUsage(prior, next provider.Usage) provider.Usage {
	pick := func(old, updated int) int {
		if updated != 0 {
			return updated
		}
		return old
	}
	return provider.Usage{
		InputTokens:        pick(prior.InputTokens, next.InputTokens),
		OutputTokens:       pick(prior.OutputTokens, next.OutputTokens),
		CacheCreationInput: pick(prior.CacheCreationInput, next.CacheCreationInput),
		CacheReadInput:     pick(prior.CacheReadInput, next.CacheReadInput),
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

func floatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func tokenUsage(value any) (message.TokenUsage, bool) {
	switch typed := value.(type) {
	case provider.Usage:
		return message.TokenUsage{InputTokens: typed.InputTokens, OutputTokens: typed.OutputTokens, CacheCreationInput: typed.CacheCreationInput, CacheReadInput: typed.CacheReadInput}, true
	case map[string]any:
		return message.TokenUsage{
			InputTokens:        intFromAny(typed["input_tokens"]),
			OutputTokens:       intFromAny(typed["output_tokens"]),
			CacheCreationInput: intFromAny(typed["cache_creation_input_tokens"]),
			CacheReadInput:     intFromAny(typed["cache_read_input_tokens"]),
		}, true
	default:
		return message.TokenUsage{}, false
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
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
