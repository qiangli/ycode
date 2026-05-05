package cli

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// TestPermissionRequestRoundTrip drives a bus.EventPermissionReq through the
// TUI and confirms that pressing y or n calls RespondPermission on the
// underlying client with the matching request ID and decision.
//
// Regression guard for the previously-stubbed `_ = reqID // TODO: wire
// RespondPermission` in tui.go — the agent would hang forever waiting for a
// decision the TUI never sent.
func TestPermissionRequestRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		decide   func(m *TUIModel)
		expected bool
	}{
		{"approve", func(m *TUIModel) { m.confirmYes() }, true},
		{"deny", func(m *TUIModel) { m.confirmNo() }, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestTUIModel(t)

			var (
				gotID      atomic.Value
				gotAllowed atomic.Value
				called     = make(chan struct{}, 1)
			)
			fake := &fakeAgentClient{
				permFunc: func(_ context.Context, requestID string, allowed bool) error {
					gotID.Store(requestID)
					gotAllowed.Store(allowed)
					select {
					case called <- struct{}{}:
					default:
					}
					return nil
				},
			}
			m.cl = fake

			data, err := json.Marshal(map[string]any{
				"request_id": "req-42",
				"tool":       "bash",
			})
			if err != nil {
				t.Fatalf("marshal event data: %v", err)
			}
			ev := bus.Event{
				Type: bus.EventPermissionReq,
				Data: data,
			}
			m.handleBusEvent(ev)
			if !m.confirming {
				t.Fatal("expected TUI to enter confirming state after permission request")
			}
			if m.confirmYes == nil || m.confirmNo == nil {
				t.Fatal("expected confirmYes and confirmNo callbacks to be set")
			}

			tc.decide(m)

			select {
			case <-called:
			case <-time.After(2 * time.Second):
				t.Fatal("RespondPermission was not called within 2s")
			}

			if got := gotID.Load(); got != "req-42" {
				t.Errorf("RespondPermission requestID = %v, want req-42", got)
			}
			if got := gotAllowed.Load(); got != tc.expected {
				t.Errorf("RespondPermission allowed = %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestPermissionRespondNoopWithoutClient confirms respondPermission is a safe
// no-op when the TUI has no client (in-process mode without the
// client/service/bus path). The in-process permission resolver consults config
// directly in that case, so the TUI helper has nothing to forward.
func TestPermissionRespondNoopWithoutClient(t *testing.T) {
	m := newTestTUIModel(t)
	m.cl = nil
	// Should not panic or block.
	m.respondPermission("req-1", true)
}
