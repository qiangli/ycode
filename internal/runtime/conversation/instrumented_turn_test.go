package conversation

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/runtime/session"
	yotel "github.com/qiangli/ycode/internal/telemetry/otel"
)

// TestInstrumentedTurnWithRecovery_RecordsTurnAndLLMMetrics pins the
// regression where the chat-runtime path (LocalService.SendMessage →
// InstrumentedTurnWithRecovery) silently emitted no turn or LLM
// metrics — only compaction. That left every panel keyed on
// ycode_session_turns / ycode_llm_call_total / ycode_turn_duration /
// ycode_turn_tool_count empty in production.
func TestInstrumentedTurnWithRecovery_RecordsTurnAndLLMMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	inst, err := yotel.NewInstruments(mp.Meter("ycode"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	rt := newTestConversationRuntime(newTextProvider("hi there"))
	rt.session = &session.Session{ID: "test-session"}
	rt.SetOTEL(&OTELConfig{
		Tracer: tracenoop.NewTracerProvider().Tracer("ycode.conversation"),
		Inst:   inst,
	})

	messages := []api.Message{
		{Role: api.RoleUser, Content: []api.ContentBlock{{Type: api.ContentTypeText, Text: "say hi"}}},
	}

	result, _, err := rt.InstrumentedTurnWithRecovery(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("InstrumentedTurnWithRecovery: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	want := map[string]bool{
		"ycode.session.turns":   false,
		"ycode.llm.call.total":  false,
		"ycode.turn.duration":   false,
		"ycode.turn.tool_count": false,
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if _, ok := want[m.Name]; ok {
				want[m.Name] = true
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("metric %q was not recorded by InstrumentedTurnWithRecovery — chat path is back to silent (regression)", name)
		}
	}
}
