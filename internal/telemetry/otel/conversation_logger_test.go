package otel

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// testProcessor captures log records for testing.
type testProcessor struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (p *testProcessor) OnEmit(_ context.Context, r *sdklog.Record) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, *r)
	return nil
}

func (p *testProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool { return true }
func (p *testProcessor) Shutdown(context.Context) error                         { return nil }
func (p *testProcessor) ForceFlush(context.Context) error                       { return nil }

func (p *testProcessor) getRecords() []sdklog.Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]sdklog.Record, len(p.records))
	copy(out, p.records)
	return out
}

func TestConversationLoggerLogConversation(t *testing.T) {
	proc := &testProcessor{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))
	t.Cleanup(func() { provider.Shutdown(nil) })

	cl := NewConversationLogger(provider, "instance-1")

	record := &ConversationRecord{
		Timestamp:        time.Now(),
		SessionID:        "sess-1",
		TurnIndex:        3,
		Provider:         "anthropic",
		Model:            "claude-sonnet-4-20250514",
		SystemPrompt:     "You are helpful.",
		Messages:         json.RawMessage(`[{"role":"user","content":"hello"}]`),
		ToolDefs:         5,
		ResponseText:     "Hi there!",
		StopReason:       "end_turn",
		TokensIn:         100,
		TokensOut:        50,
		EstimatedCostUSD: 0.003,
		DurationMs:       1200,
		Success:          true,
	}

	cl.LogConversation(record)

	recs := proc.getRecords()
	if len(recs) == 0 {
		t.Fatal("expected at least one log record")
	}

	found := false
	for _, rec := range recs {
		rec.WalkAttributes(func(kv log.KeyValue) bool {
			if kv.Key == "log.type" && kv.Value.AsString() == "conversation" {
				found = true
				return false
			}
			return true
		})
	}
	if !found {
		t.Fatal("expected log record with log.type=conversation")
	}
}

func TestConversationLoggerLogToolCall(t *testing.T) {
	proc := &testProcessor{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))
	t.Cleanup(func() { provider.Shutdown(nil) })

	cl := NewConversationLogger(provider, "instance-2")

	tc := ToolCallLog{
		Name:       "write_file",
		Input:      json.RawMessage(`{"path":"test.go"}`),
		Output:     "OK",
		Success:    true,
		DurationMs: 30,
	}

	cl.LogToolCall("sess-2", 1, tc)

	recs := proc.getRecords()
	if len(recs) == 0 {
		t.Fatal("expected at least one log record")
	}

	found := false
	for _, rec := range recs {
		rec.WalkAttributes(func(kv log.KeyValue) bool {
			if kv.Key == "log.type" && kv.Value.AsString() == "tool_call" {
				found = true
				return false
			}
			return true
		})
	}
	if !found {
		t.Fatal("expected log record with log.type=tool_call")
	}
}

func TestConversationLoggerNilRecord(t *testing.T) {
	proc := &testProcessor{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))
	t.Cleanup(func() { provider.Shutdown(nil) })

	cl := NewConversationLogger(provider, "instance-3")
	cl.LogConversation(nil) // should not panic

	if len(proc.getRecords()) != 0 {
		t.Fatal("nil record should not emit a log")
	}
}
