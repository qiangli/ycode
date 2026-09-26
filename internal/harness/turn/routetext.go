package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/provider"
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
			response, outcome := collectProvider(attemptCtx, adapter.Send(attemptCtx, request))
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

// emptyResponse reports a completed response that carries nothing to act on:
// no text and no tool call.
func emptyResponse(response map[string]any) bool {
	hasTools, _ := response["hasToolCalls"].(bool)
	return !hasTools && len(anyList(response["toolCalls"])) == 0 && strings.TrimSpace(text(response["text"])) == ""
}
