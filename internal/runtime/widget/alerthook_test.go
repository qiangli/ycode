package widget

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestAlertHook_RendersIframeOnFire(t *testing.T) {
	b := bus.NewMemoryBus()
	ch, unsub := b.Subscribe(bus.EventStateUpdate)
	defer unsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hook := NewAlertHook(b, "sess-target")
	hook.Start(ctx)
	// Give Start's goroutine a tick to install the subscription before we
	// publish — otherwise the alert event can race the subscriber.
	time.Sleep(20 * time.Millisecond)

	bus.PublishAlertFired(b, bus.AlertFiredPayload{
		Name:        "HighLatency",
		Severity:    "critical",
		Summary:     "p99 over 500ms",
		Description: "auth-service p99 latency is 700ms over the last 5m",
		Labels:      map[string]string{"service": "auth", "env": "prod"},
		Annotations: map[string]string{"runbook": "https://example/runbook"},
		Value:       0.7,
		Source:      "prometheus",
	})

	select {
	case ev := <-ch:
		if ev.SessionID != "sess-target" {
			t.Errorf("session = %q want sess-target", ev.SessionID)
		}
		var p iframePayload
		if err := json.Unmarshal(ev.Data, &p); err != nil {
			t.Fatal(err)
		}
		if p.Format != "iframe" {
			t.Errorf("format = %q want iframe", p.Format)
		}
		if p.WidgetID != "incident-HighLatency" {
			t.Errorf("widget_id = %q want incident-HighLatency", p.WidgetID)
		}
		if p.Origin != "alert:prometheus" {
			t.Errorf("origin = %q want alert:prometheus", p.Origin)
		}
		// Spot-check that the rendered HTML actually contains the alert
		// metadata — if the template ever silently drops fields, this
		// fails loudly.
		for _, want := range []string{"HighLatency", "p99 over 500ms", "auth", "CRITICAL", "runbook"} {
			if !strings.Contains(p.HTML, want) {
				t.Errorf("incident HTML missing %q", want)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no state.update published after alert fire")
	}
}

func TestAlertHook_DefaultsToCanvasDefaultSession(t *testing.T) {
	b := bus.NewMemoryBus()
	ch, unsub := b.Subscribe(bus.EventStateUpdate)
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	NewAlertHook(b, "").Start(ctx)
	time.Sleep(20 * time.Millisecond)

	bus.PublishAlertFired(b, bus.AlertFiredPayload{Name: "X", Severity: "warning"})

	select {
	case ev := <-ch:
		if ev.SessionID != DefaultSession {
			t.Errorf("empty session arg should fall back to %q, got %q", DefaultSession, ev.SessionID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
}

func TestAlertHook_NilBusIsNoOp(t *testing.T) {
	// Construction + Start with a nil bus must not panic — call-sites in
	// serve.go skip wiring when the API stack is unavailable, but Start
	// might still be reached if the wiring order is sloppy.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil-bus Start panicked: %v", r)
		}
	}()
	NewAlertHook(nil, "x").Start(context.Background())
}

func TestRenderIncidentHTML_EscapesUserContent(t *testing.T) {
	// Alert labels and annotations are external input (Prometheus rule
	// authors). If they're rendered without escaping, an attacker who
	// can write alert rules can inject script into the iframe. The
	// iframe is sandboxed so the blast radius is small, but escaping
	// is still the right baseline.
	out := RenderIncidentHTML(bus.AlertFiredPayload{
		Name:    `<img onerror="x()">`,
		Summary: `</style><script>alert(1)</script>`,
		Labels:  map[string]string{"a": `<x>`},
	})
	if strings.Contains(out, "<script>alert(1)") {
		t.Errorf("incident HTML did not escape <script> from summary: %s", out)
	}
	if strings.Contains(out, `onerror="x()"`) {
		t.Errorf("incident HTML did not escape onerror= from name: %s", out)
	}
	if strings.Contains(out, "<x>") && !strings.Contains(out, "&lt;x&gt;") {
		t.Errorf("incident HTML did not escape label value: %s", out)
	}
}

func TestRenderIncidentHTML_HandlesEmptyAlert(t *testing.T) {
	// An alert with empty name/summary/labels shouldn't crash the
	// template — it should produce a minimal but valid overlay.
	out := RenderIncidentHTML(bus.AlertFiredPayload{})
	if !strings.HasPrefix(out, "<!DOCTYPE html>") {
		t.Errorf("output should be a complete HTML doc, got: %.80s", out)
	}
	if !strings.Contains(out, "unnamed") {
		t.Errorf("missing-name fallback should mention 'unnamed', got: %.200s", out)
	}
}

func TestAlertHook_ReFireReplacesByWidgetID(t *testing.T) {
	// Two fires of the same alert should produce two state.update
	// events both keyed by the same widget_id — canvas.js then replaces
	// in place rather than appending. This is the user-facing
	// equivalent of Alertmanager's grouping/dedup.
	b := bus.NewMemoryBus()
	ch, unsub := b.Subscribe(bus.EventStateUpdate)
	defer unsub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	NewAlertHook(b, "s").Start(ctx)
	time.Sleep(20 * time.Millisecond)

	bus.PublishAlertFired(b, bus.AlertFiredPayload{Name: "Same", Severity: "info"})
	bus.PublishAlertFired(b, bus.AlertFiredPayload{Name: "Same", Severity: "info"})

	got := 0
	deadline := time.After(2 * time.Second)
	for got < 2 {
		select {
		case ev := <-ch:
			var p iframePayload
			_ = json.Unmarshal(ev.Data, &p)
			if p.WidgetID != "incident-Same" {
				t.Errorf("widget_id = %q want incident-Same", p.WidgetID)
			}
			got++
		case <-deadline:
			t.Fatalf("only got %d events, want 2", got)
		}
	}
}
