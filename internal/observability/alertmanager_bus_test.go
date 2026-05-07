package observability

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	am "github.com/qiangli/ycode/pkg/otel/alertmanager"

	prommodel "github.com/prometheus/common/model"
)

// TestAlertmanager_BusTap verifies that AddAlert publishes an
// EventAlertFired onto a configured bus alongside the standard
// Alertmanager-internal delivery — the foundation of the self-healing
// alert path.
func TestAlertmanager_BusTap(t *testing.T) {
	b := bus.NewMemoryBus()
	defer b.Close()

	comp := NewAlertmanagerComponent()
	comp.SetBus(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer comp.Stop(ctx)

	ch, unsub := b.Subscribe(bus.EventAlertFired)
	defer unsub()

	alert := &am.Alert{
		Alert: prommodel.Alert{
			Labels: prommodel.LabelSet{
				"alertname": "HighErrorRate",
				"severity":  "warning",
				"service":   "ycode",
			},
			Annotations: prommodel.LabelSet{
				"summary":     "error rate above threshold",
				"description": "more than 5 errors per minute",
			},
			StartsAt: time.Now(),
		},
	}
	if err := comp.AddAlert(ctx, alert); err != nil {
		t.Fatalf("AddAlert: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.Type != bus.EventAlertFired {
			t.Errorf("event type = %q, want alert.fired", ev.Type)
		}
		var got bus.AlertFiredPayload
		if err := json.Unmarshal(ev.Data, &got); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if got.Name != "HighErrorRate" {
			t.Errorf("Name = %q", got.Name)
		}
		if got.Severity != "warning" {
			t.Errorf("Severity = %q", got.Severity)
		}
		if got.Summary != "error rate above threshold" {
			t.Errorf("Summary = %q", got.Summary)
		}
		if got.Labels["service"] != "ycode" {
			t.Errorf("labels missing service: %v", got.Labels)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive EventAlertFired within 2s")
	}
}

func TestAlertmanager_NoBusIsNoop(t *testing.T) {
	// Without a bus configured, AddAlert must still work (the in-memory
	// AM provider receives the alert) without panicking.
	comp := NewAlertmanagerComponent()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer comp.Stop(ctx)

	alert := &am.Alert{
		Alert: prommodel.Alert{
			Labels:   prommodel.LabelSet{"alertname": "X"},
			StartsAt: time.Now(),
		},
	}
	if err := comp.AddAlert(ctx, alert); err != nil {
		t.Fatalf("AddAlert without bus: %v", err)
	}
}
