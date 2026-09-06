package ioctx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/qiangli/ycode/internal/harness/spec"
)

type OutputRequest struct {
	EventID        string
	OriginFrontend string
	SinkRefs       []string
	Content        []byte
}

type DeliveryResult struct {
	SinkRef    string `json:"sink_ref"`
	PayloadRef string `json:"payload_ref"`
	Attempts   int    `json:"attempts"`
}

type Output struct {
	SchemaVersion string           `json:"schema_version"`
	EventID       string           `json:"event_id"`
	Deliveries    []DeliveryResult `json:"deliveries"`
}

// EmitStage binds delivery routing to the ordered sinkRefs list compiled from
// run.with; map iteration never chooses a destination or its order.
func (e *Engine) EmitStage(ctx context.Context, meta Meta, run spec.Run, eventID, originFrontend string, content []byte) (Output, error) {
	if run.Stage != "output.emit" || run.In["messages"] == "" {
		return Output{}, errors.New("output emit: invalid compiled stage ports")
	}
	sinkRefs, err := stringList(run.With["sinkRefs"])
	if err != nil {
		return Output{}, fmt.Errorf("output emit: %w", err)
	}
	return e.Emit(ctx, meta, OutputRequest{EventID: eventID, OriginFrontend: originFrontend, SinkRefs: sinkRefs, Content: content})
}

// Emit routes output through the sink refs in their declared list order. Each
// external attempt is bracketed by durable events, and bytes live in the
// content-addressed payload store so replay never depends on the destination.
func (e *Engine) Emit(ctx context.Context, meta Meta, request OutputRequest) (Output, error) {
	if err := meta.validate(); err != nil {
		return Output{}, err
	}
	if request.EventID == "" || request.OriginFrontend == "" || len(request.SinkRefs) == 0 || len(request.Content) == 0 {
		return Output{}, errors.New("output emit requires event id, origin frontend, ordered sinks and content")
	}
	if e.delivery == nil || e.redactor == nil || e.deadLetter == nil {
		return Output{}, errors.New("output emit requires delivery, redactor and dead-letter mechanisms")
	}
	result := Output{SchemaVersion: SchemaVersion, EventID: request.EventID}
	seen := make(map[string]struct{}, len(request.SinkRefs))
	for position, sinkRef := range request.SinkRefs {
		if _, duplicate := seen[sinkRef]; duplicate {
			return Output{}, fmt.Errorf("output emit: duplicate sink ref %q", sinkRef)
		}
		seen[sinkRef] = struct{}{}
		sink, ok := e.sinks[sinkRef]
		if !ok || sink.Kind != "frontend-response" || sink.Delivery.Mode != "at-least-once" || sink.Delivery.DeduplicateBy != "event-id" {
			return Output{}, fmt.Errorf("output emit: invalid compiled sink %q", sinkRef)
		}
		if !contains(sink.FrontendRefs, request.OriginFrontend) || !contains(sink.DestinationAllowlist, "originating-frontend") {
			return Output{}, fmt.Errorf("output emit: origin frontend %q is not allowed by sink %q", request.OriginFrontend, sinkRef)
		}
		if sink.Delivery.Retry.MaxAttempts <= 0 || sink.Delivery.Retry.BackoffMS < 0 || sink.Delivery.Retry.MaxBackoffMS < sink.Delivery.Retry.BackoffMS {
			return Output{}, fmt.Errorf("output emit: invalid retry configuration for sink %q", sinkRef)
		}
		content, err := e.redactor.Redact(sink.Redact, append([]byte(nil), request.Content...))
		if err != nil {
			return Output{}, fmt.Errorf("output emit: redact for sink %q: %w", sinkRef, err)
		}
		ref, err := e.payloads.Put(content)
		if err != nil {
			return Output{}, err
		}
		deliveryRequest := DeliveryRequest{EventID: request.EventID, SinkRef: sinkRef, FrontendRef: request.OriginFrontend, PayloadRef: ref, Content: content}
		attempts, err := e.deliver(ctx, meta, position, sinkRef, sink.Delivery.Retry.MaxAttempts, sink.Delivery.Retry.BackoffMS, sink.Delivery.Retry.MaxBackoffMS, sink.Delivery.DeadLetter.ControlPath, deliveryRequest)
		if err != nil {
			return Output{}, err
		}
		result.Deliveries = append(result.Deliveries, DeliveryResult{SinkRef: sinkRef, PayloadRef: ref, Attempts: attempts})
	}
	_, err := e.append(meta, "output.emitted", map[string]any{"schema_version": SchemaVersion, "event_id": request.EventID, "origin_frontend": request.OriginFrontend, "deliveries": result.Deliveries})
	return result, err
}

func stringList(value any) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("compiled list contains a non-string")
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, errors.New("compiled value is not an explicit list")
	}
}

func (e *Engine) deliver(ctx context.Context, meta Meta, position int, sinkRef string, maxAttempts, backoffMS, maxBackoffMS int, deadLetterPath string, request DeliveryRequest) (int, error) {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := e.append(meta, "output.delivery.requested", map[string]any{"schema_version": SchemaVersion, "event_id": request.EventID, "sink_ref": sinkRef, "sink_position": position, "frontend_ref": request.FrontendRef, "payload_ref": request.PayloadRef, "attempt": attempt})
		if err != nil {
			return attempt - 1, err
		}
		err = e.delivery.Deliver(ctx, request)
		if err == nil {
			_, appendErr := e.append(meta, "output.delivery.completed", map[string]any{"schema_version": SchemaVersion, "event_id": request.EventID, "sink_ref": sinkRef, "sink_position": position, "frontend_ref": request.FrontendRef, "payload_ref": request.PayloadRef, "attempt": attempt})
			return attempt, appendErr
		}
		_, appendErr := e.append(meta, "output.delivery.failed", map[string]any{"schema_version": SchemaVersion, "event_id": request.EventID, "sink_ref": sinkRef, "sink_position": position, "frontend_ref": request.FrontendRef, "payload_ref": request.PayloadRef, "attempt": attempt, "error": err.Error()})
		if appendErr != nil {
			return attempt, appendErr
		}
		if attempt == maxAttempts {
			if deadLetterPath == "" {
				return attempt, fmt.Errorf("output emit: sink %q has no dead-letter path after %d attempts: %w", sinkRef, attempt, err)
			}
			deadLetter := DeadLetterRequest{ControlPath: deadLetterPath, Delivery: request, Cause: err.Error()}
			if storeErr := e.deadLetter.Store(ctx, deadLetter); storeErr != nil {
				return attempt, fmt.Errorf("output emit: sink %q dead-letter: %w", sinkRef, storeErr)
			}
			if _, appendErr := e.append(meta, "output.dead-lettered", map[string]any{"schema_version": SchemaVersion, "event_id": request.EventID, "sink_ref": sinkRef, "sink_position": position, "frontend_ref": request.FrontendRef, "payload_ref": request.PayloadRef, "attempts": attempt, "control_path": deadLetterPath, "error": err.Error()}); appendErr != nil {
				return attempt, appendErr
			}
			return attempt, fmt.Errorf("output emit: sink %q exhausted %d attempts: %w", sinkRef, attempt, err)
		}
		delay := backoffMS
		for i := 1; i < attempt && delay < maxBackoffMS; i++ {
			delay *= 2
			if delay > maxBackoffMS {
				delay = maxBackoffMS
			}
		}
		timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return attempt, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}
