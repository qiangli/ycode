package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func (r *Runtime) RouteText(ctx context.Context, routeRef, system string, messages []message.Message) (string, error) {
	response, outcome := r.routeProvider(ctx, "memory.compact", routeRef, system, messages, nil)
	if outcome.Class == provider.OutcomeCompleted || outcome.Class == provider.OutcomeLimit {
		if text(response["text"]) == "" {
			return "", errors.New("route text: provider returned empty text")
		}
		return text(response["text"]), nil
	}
	if outcome.Error == "" {
		outcome.Error = string(outcome.Class)
	}
	return "", errors.New(outcome.Error)
}

func (r *Runtime) routeProvider(ctx context.Context, stageID, routeRef, system string, messages []message.Message, providerSession any) (map[string]any, provider.Outcome) {
	route, ok := r.doc.Spec.Routes[routeRef]
	if !ok || len(route.Attempts) == 0 {
		return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: fmt.Sprintf("invalid route %q", routeRef)}
	}
	requestMessages, extractedSystem := providerMessages(messages)
	run, _ := runFrom(ctx)
	// Select only a declared attempt, preserving its timeout/retry/budget.
	// Summary routes retain their own YAML model selection.
	if modelRef := sessionModelRef(run, r.doc, routeRef); modelRef != "" {
		found := false
		for _, attempt := range route.Attempts {
			if attempt.ModelRef == modelRef {
				route.Attempts = []spec.RouteAttempt{attempt}
				found = true
				break
			}
		}
		if !found {
			return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: "session model is not in the declared route"}
		}
	}
	if system == "" {
		system = extractedSystem
	}
	var last provider.Outcome
	for attemptIndex, attempt := range route.Attempts {
		model, ok := r.doc.Spec.Models[attempt.ModelRef]
		if !ok {
			return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: fmt.Sprintf("undeclared model %q", attempt.ModelRef)}
		}
		adapter := r.providers[model.ProviderRef]
		if adapter == nil {
			return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: fmt.Sprintf("unavailable provider %q", model.ProviderRef)}
		}
		maxAttempts := attempt.TransportRetry.MaxAttempts
		if maxAttempts < 1 {
			return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: fmt.Sprintf("route %q has invalid retry count", routeRef)}
		}
		for transportAttempt := 1; transportAttempt <= maxAttempts; transportAttempt++ {
			maxTokens := route.Budget.MaxOutputTokens
			if maxTokens > model.Limits.MaxOutputTokens {
				maxTokens = model.Limits.MaxOutputTokens
			}
			request := provider.Request{Model: model.ID, System: system, Messages: requestMessages, MaxTokens: maxTokens, Stream: model.Capabilities.Streaming, BashyTool: model.Capabilities.ToolCalls}
			// A plan turn and a memory.compact summarization both ask the model
			// for plain text, never a command: offering the tool lets a
			// tool-capable model answer with a bashy call instead of text, which
			// RouteText (and the plan gate below) treats as a failed turn. For
			// compaction that meant the deterministic fallback firing on almost
			// every compaction instead of the rare real failure it exists for.
			if run.plan || stageID == "memory.compact" {
				request.BashyTool = false
			}
			requestRef, err := r.payload(request)
			if err != nil {
				return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: err.Error()}
			}
			_ = r.append(ctx, stageID, "llm.requested", map[string]any{"route_ref": routeRef, "model_ref": attempt.ModelRef, "attempt": transportAttempt, "payload_ref": requestRef})
			attemptCtx := ctx
			cancel := func() {}
			if attempt.TimeoutMS > 0 {
				attemptCtx, cancel = context.WithTimeout(ctx, time.Duration(attempt.TimeoutMS)*time.Millisecond)
			}
			// llm.delta announces the provider's text live by payload_ref (raw
			// text stays in the payload store, never inline in the journal);
			// llm.completed's payload stays the canonical response.
			delta := func(channel, chunk string) error {
				deltaRef, err := r.payload(map[string]any{"channel": channel, "text": chunk})
				if err != nil {
					return fmt.Errorf("llm.delta payload: %w", err)
				}
				if err := r.append(ctx, stageID, "llm.delta", map[string]any{"route_ref": routeRef, "model_ref": attempt.ModelRef, "attempt": transportAttempt, "channel": channel, "payload_ref": deltaRef}); err != nil {
					return fmt.Errorf("llm.delta append: %w", err)
				}
				return nil
			}
			response, outcome := collectProvider(attemptCtx, adapter.Send(attemptCtx, request), delta)
			if run.plan && (outcome.Class == provider.OutcomeToolCall || len(anyList(response["toolCalls"])) != 0) {
				outcome = provider.Outcome{Class: provider.OutcomeProtocolError, Error: "planning forbids tool calls"}
			}
			cancel()
			// A small thinking model can spend the whole output budget on
			// reasoning it never shows: that limit is as empty as a completion.
			if (outcome.Class == provider.OutcomeCompleted || outcome.Class == provider.OutcomeLimit) && emptyResponse(response) {
				outcome = provider.Outcome{Class: provider.OutcomeEmpty, Error: "provider response has no content"}
			}
			responseRef, err := r.payload(map[string]any{"response": response, "outcome": outcome})
			if err != nil {
				return nil, provider.Outcome{Class: provider.OutcomeProtocolError, Error: err.Error()}
			}
			_ = r.append(ctx, stageID, "llm.completed", map[string]any{"route_ref": routeRef, "model_ref": attempt.ModelRef, "attempt": transportAttempt, "payload_ref": responseRef, "outcome": outcome.Class})
			last = outcome
			if outcome.Class == provider.OutcomeCompleted || outcome.Class == provider.OutcomeToolCall || outcome.Class == provider.OutcomeLimit {
				response["model"] = model.ID
				if outcome.Class == provider.OutcomeLimit {
					response["finished"] = true
				}
				return response, outcome
			}
			if transportAttempt == maxAttempts || !retryableProvider(outcome.Class, attempt.TransportRetry.RetryOn) {
				break
			}
			if err := waitBackoff(ctx, attempt.TransportRetry.Backoff, transportAttempt); err != nil {
				return nil, provider.Outcome{Class: provider.OutcomeCanceled, Error: err.Error()}
			}
		}
		if attemptIndex+1 == len(route.Attempts) || !retryableProvider(last.Class, route.FallbackOn) {
			break
		}
	}
	_ = providerSession
	return nil, last
}

// sessionModelRef reports the model ref a session selected for routeRef
// (ycode model use / turn.Request.ModelRef), or "" when the turn uses the
// route's declared default. It applies only to the agent's own model route:
// a summary route (memory.compact) keeps its own YAML model selection
// regardless of what the session picked for conversation turns. Every
// consumer of route.Attempts[0] as "the model for this turn" (inference in
// routeProvider, context budgeting in the measure stage) must resolve
// through this instead, or a session override silently stops applying to it.
func sessionModelRef(run runContext, doc *spec.Document, routeRef string) string {
	if run.modelRef == "" || routeRef != doc.Spec.Agents[run.agentRef].ModelRouteRef {
		return ""
	}
	return run.modelRef
}

// emptyResponse reports a completed response that carries nothing to act on:
// no text and no tool call.
func emptyResponse(response map[string]any) bool {
	hasTools, _ := response["hasToolCalls"].(bool)
	return !hasTools && len(anyList(response["toolCalls"])) == 0 && strings.TrimSpace(text(response["text"])) == ""
}
