package provider

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	api "github.com/qiangli/ycode/internal/api"
)

func TestAdaptersNormalizeOneBashyStream(t *testing.T) {
	t.Parallel()
	kinds := []Kind{KindMock, KindAnthropic, KindOpenAICompatible, KindGemini}
	for _, kind := range kinds {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			backend := &MockBackend{Events: canonicalWireEvents("provider-call")}
			adapter, err := New(kind, backend)
			if err != nil {
				t.Fatal(err)
			}
			events := collect(adapter.Send(context.Background(), testRequest()))
			assertCanonicalEvents(t, events, "provider-call")

			wire := backend.LastRequest()
			if wire == nil || len(wire.Tools) != 1 || wire.Tools[0].Name != ToolName {
				t.Fatalf("provider tools = %#v, want sole bashy", wire)
			}
			if !wire.Stream {
				t.Fatal("stream preference was not preserved")
			}
			if got := wire.Messages[0].Content[0].Text; got != "hello" {
				t.Fatalf("message content = %q", got)
			}
		})
	}
}

func TestMissingCallIDIsDeterministic(t *testing.T) {
	t.Parallel()
	run := func() string {
		backend := &MockBackend{Events: canonicalWireEvents("")}
		adapter, err := NewMock(backend)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range collect(adapter.Send(context.Background(), testRequest())) {
			if event.ToolCall != nil {
				return event.ToolCall.ID
			}
		}
		return ""
	}
	first, second := run(), run()
	if first == "" || first != second {
		t.Fatalf("call ids = %q, %q; want equal non-empty ids", first, second)
	}
}

func TestForbiddenProviderToolBecomesProtocolOutcome(t *testing.T) {
	t.Parallel()
	backend := &MockBackend{Events: []*api.StreamEvent{
		{Type: "content_block_start", ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: "x", Name: "read_file", Input: json.RawMessage(`{}`)}},
		{Type: "content_block_stop"},
		{Type: "message_stop"},
	}}
	adapter, _ := NewMock(backend)
	events := collect(adapter.Send(context.Background(), testRequest()))
	if got := events[len(events)-1].Outcome; got == nil || got.Class != OutcomeProtocolError {
		t.Fatalf("outcome = %#v, want protocol error", got)
	}
	for _, event := range events {
		if event.ToolCall != nil {
			t.Fatalf("forbidden tool escaped normalization: %#v", event.ToolCall)
		}
	}
}

func TestProviderFailureAndCancellationAreOutcomes(t *testing.T) {
	t.Parallel()
	t.Run("provider error", func(t *testing.T) {
		backend := &MockBackend{Err: errors.New("upstream unavailable")}
		adapter, _ := NewMock(backend)
		events := collect(adapter.Send(context.Background(), testRequest()))
		if got := events[len(events)-1].Outcome; got == nil || got.Class != OutcomeProviderError {
			t.Fatalf("outcome = %#v", got)
		}
	})

	t.Run("deadline", func(t *testing.T) {
		adapter, _ := New(KindMock, blockingBackend{})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		events := collect(adapter.Send(ctx, testRequest()))
		if got := events[len(events)-1].Outcome; got == nil || got.Class != OutcomeDeadline {
			t.Fatalf("outcome = %#v", got)
		}
	})
}

func TestInvalidRequestIsProtocolOutcomeWithoutCallingProvider(t *testing.T) {
	t.Parallel()
	backend := &MockBackend{}
	adapter, _ := NewMock(backend)
	events := collect(adapter.Send(context.Background(), Request{}))
	if got := events[len(events)-1].Outcome; got == nil || got.Class != OutcomeProtocolError {
		t.Fatalf("outcome = %#v", got)
	}
	if backend.LastRequest() != nil {
		t.Fatal("invalid request reached provider")
	}
}

func canonicalWireEvents(id string) []*api.StreamEvent {
	text, _ := json.Marshal(map[string]string{"type": "text_delta", "text": "working"})
	partial, _ := json.Marshal(map[string]string{"type": "input_json_delta", "partial_json": `{"script":"echo hi"}`})
	stop, _ := json.Marshal(map[string]string{"stop_reason": api.StopReasonToolUse})
	return []*api.StreamEvent{
		{Type: "message_start", Message: &api.Response{Usage: api.Usage{InputTokens: 11, CacheReadInput: 3}}},
		{Type: "content_block_delta", Delta: text},
		{Type: "content_block_start", Index: 1, ContentBlock: &api.ContentBlock{Type: api.ContentTypeToolUse, ID: id, Name: ToolName, Input: json.RawMessage(`{}`)}},
		{Type: "content_block_delta", Index: 1, Delta: partial},
		{Type: "content_block_stop", Index: 1},
		{Type: "message_delta", Usage: &api.Usage{OutputTokens: 7}, Delta: stop},
		{Type: "message_stop"},
	}
}

func testRequest() Request {
	return Request{
		Model: "test-model", MaxTokens: 128, Stream: true,
		Messages: []api.Message{{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "hello"}}}},
	}
}

func assertCanonicalEvents(t *testing.T, events []Event, callID string) {
	t.Helper()
	gotTypes := make([]EventType, len(events))
	for i := range events {
		gotTypes[i] = events[i].Type
	}
	wantTypes := []EventType{EventUsage, EventTextDelta, EventToolCall, EventUsage, EventOutcome}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types = %v, want %v", gotTypes, wantTypes)
	}
	if got := events[2].ToolCall; got == nil || got.ID != callID || got.Name != ToolName || string(got.Input) != `{"script":"echo hi"}` {
		t.Fatalf("tool call = %#v", got)
	}
	if got := events[4].Outcome; got == nil || got.Class != OutcomeToolCall || got.StopReason != api.StopReasonToolUse {
		t.Fatalf("outcome = %#v", got)
	}
}

func collect(stream <-chan Event) []Event {
	var result []Event
	for event := range stream {
		result = append(result, event)
	}
	return result
}

type blockingBackend struct{}

func (blockingBackend) Kind() api.ProviderKind { return api.ProviderLocal }
func (blockingBackend) Send(ctx context.Context, _ *api.Request) (<-chan *api.StreamEvent, <-chan error) {
	events := make(chan *api.StreamEvent)
	errs := make(chan error)
	go func() {
		<-ctx.Done()
		close(events)
		close(errs)
	}()
	return events, errs
}
