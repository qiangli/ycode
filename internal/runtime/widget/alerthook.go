package widget

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// AlertHook bridges Prometheus alert evaluation into the canvas. It
// subscribes to bus.EventAlertFired and publishes a bus.EventStateUpdate
// carrying an iframe-format incident overlay onto a target session.
// /canvas/ subscribers (and any foreign tool listening on the session)
// render the overlay automatically.
//
// v1 ships a static HTML template — alert name, severity, summary,
// description, labels — so the canvas demo works end-to-end on day one
// without requiring the runtime agent to be wired into the alert
// pipeline. v1.5 replaces this with an agent-composed overlay that
// adds correlated logs (via VictoriaLogs), recent commits, and a
// suggested first action.
//
// One alert fires → one widget_id (`incident-<name>`). Re-firing the
// same alert replaces the overlay rather than appending — matches
// Alertmanager's grouping semantics for the user-facing layer.
type AlertHook struct {
	bus       bus.Bus
	sessionID string
	logger    *slog.Logger
}

// NewAlertHook returns a hook that renders incident overlays onto the
// given session. Empty sessionID falls back to DefaultSession so the
// trivial-case demo (open /canvas/ with no ?session=) works without
// any wiring.
func NewAlertHook(b bus.Bus, sessionID string) *AlertHook {
	if sessionID == "" {
		sessionID = DefaultSession
	}
	return &AlertHook{
		bus:       b,
		sessionID: sessionID,
		logger:    slog.Default(),
	}
}

// Start subscribes to alert events and renders overlays until ctx is
// done. Returns immediately after registering the subscription; the
// goroutine exits cleanly on context cancellation.
func (h *AlertHook) Start(ctx context.Context) {
	if h.bus == nil {
		return
	}
	ch, unsub := h.bus.Subscribe(bus.EventAlertFired)
	go func() {
		defer unsub()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				h.handle(ev)
			}
		}
	}()
}

// handle is package-internal so tests can drive it directly without
// having to publish through the bus (faster + deterministic).
func (h *AlertHook) handle(ev bus.Event) {
	var p bus.AlertFiredPayload
	if err := json.Unmarshal(ev.Data, &p); err != nil {
		h.logger.Warn("alerthook: bad payload", "error", err)
		return
	}
	overlayHTML := RenderIncidentHTML(p)
	payload, err := json.Marshal(iframePayload{
		Format:   "iframe",
		WidgetID: incidentWidgetID(p),
		HTML:     overlayHTML,
		Origin:   alertOrigin(p),
	})
	if err != nil {
		h.logger.Warn("alerthook: marshal payload", "error", err)
		return
	}
	h.bus.Publish(bus.Event{
		Type:      bus.EventStateUpdate,
		SessionID: h.sessionID,
		Timestamp: time.Now(),
		Data:      payload,
	})
}

// incidentWidgetID derives a stable widget ID per alert name. Re-fires
// of the same alert collapse to one overlay (Alertmanager-style
// grouping at the user-facing layer).
func incidentWidgetID(p bus.AlertFiredPayload) string {
	name := p.Name
	if name == "" {
		name = "unnamed"
	}
	return "incident-" + name
}

// alertOrigin builds the `via …` attribution shown on the widget header.
// Prefer Source (Prometheus, Alertmanager, etc.) when set; fall back to
// a generic "alertmanager" label so the user can tell at a glance that
// this overlay came from the alert pipeline rather than an agent call.
func alertOrigin(p bus.AlertFiredPayload) string {
	if p.Source != "" {
		return "alert:" + p.Source
	}
	return "alert"
}

// RenderIncidentHTML produces the static incident overlay HTML for a
// fired alert. Exported so foreign consumers (tests, alternative
// renderers, future agent-composed variants) can reuse the template.
//
// The HTML is self-contained (inline <style>), targeted at the
// sandboxed iframe the canvas wraps every widget in. No external
// fetches, no JS, no postMessage — it's a passive overlay. The agent
// can replace it with a richer interactive widget by emitting a new
// payload with the same widget_id.
func RenderIncidentHTML(p bus.AlertFiredPayload) string {
	sev := strings.ToLower(p.Severity)
	if sev == "" {
		sev = "info"
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><style>`)
	b.WriteString(incidentCSS)
	b.WriteString(`</style></head><body>`)
	b.WriteString(fmt.Sprintf(`<div class="incident sev-%s">`, html.EscapeString(sev)))
	// Header row: severity chip + alert name + timestamp
	b.WriteString(`<header class="hdr">`)
	b.WriteString(fmt.Sprintf(`<span class="sev">%s</span>`, html.EscapeString(strings.ToUpper(sev))))
	b.WriteString(fmt.Sprintf(`<span class="name">%s</span>`, html.EscapeString(orFallback(p.Name, "unnamed alert"))))
	if !p.StartsAt.IsZero() {
		b.WriteString(fmt.Sprintf(`<span class="ts" title="%s">started %s ago</span>`,
			html.EscapeString(p.StartsAt.Format(time.RFC3339)),
			html.EscapeString(humanizeSince(p.StartsAt))))
	}
	b.WriteString(`</header>`)

	// Summary line.
	if p.Summary != "" {
		b.WriteString(fmt.Sprintf(`<p class="summary">%s</p>`, html.EscapeString(p.Summary)))
	}

	// Description (multi-line, preserve breaks).
	if p.Description != "" {
		desc := html.EscapeString(p.Description)
		desc = strings.ReplaceAll(desc, "\n", "<br>")
		b.WriteString(fmt.Sprintf(`<p class="desc">%s</p>`, desc))
	}

	// Value at firing time (when set).
	if p.Value != 0 {
		b.WriteString(fmt.Sprintf(`<p class="value">Value: <code>%g</code></p>`, p.Value))
	}

	// Labels table — small, key/value, sorted for stable diffs.
	if len(p.Labels) > 0 {
		b.WriteString(`<table class="kv"><caption>Labels</caption><tbody>`)
		for _, k := range sortedKeys(p.Labels) {
			b.WriteString(fmt.Sprintf(`<tr><th>%s</th><td>%s</td></tr>`,
				html.EscapeString(k), html.EscapeString(p.Labels[k])))
		}
		b.WriteString(`</tbody></table>`)
	}

	// Annotations — same shape as labels but typically free-form metadata.
	if len(p.Annotations) > 0 {
		b.WriteString(`<table class="kv"><caption>Annotations</caption><tbody>`)
		for _, k := range sortedKeys(p.Annotations) {
			b.WriteString(fmt.Sprintf(`<tr><th>%s</th><td>%s</td></tr>`,
				html.EscapeString(k), html.EscapeString(p.Annotations[k])))
		}
		b.WriteString(`</tbody></table>`)
	}

	// Footer hint for v1.5 — agent-composed version will replace this.
	b.WriteString(`<footer class="hint">Static incident overlay. Agent-composed version (correlated logs, recent diffs, suggested action) lands in v1.5.</footer>`)

	b.WriteString(`</div></body></html>`)
	return b.String()
}

func orFallback(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func humanizeSince(t time.Time) string {
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Small map, simple sort.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// incidentCSS is inlined so the iframe needs no external fetch. Kept
// minimal — colors come from a small severity palette and the rest is
// system fonts + simple grid layout.
const incidentCSS = `
* { box-sizing: border-box; }
body { margin: 0; font: 13px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #1f2328; background: #fafafa; }
.incident { padding: 16px 20px; border-left: 4px solid #6b7280; }
.incident.sev-info { border-left-color: #3b82f6; background: #eff6ff; }
.incident.sev-warning { border-left-color: #f59e0b; background: #fffbeb; }
.incident.sev-critical { border-left-color: #dc2626; background: #fef2f2; }
.hdr { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.sev { font-size: 10px; font-weight: 700; padding: 2px 8px; border-radius: 4px; background: #1f2328; color: white; letter-spacing: 0.05em; }
.incident.sev-info .sev { background: #3b82f6; }
.incident.sev-warning .sev { background: #f59e0b; }
.incident.sev-critical .sev { background: #dc2626; }
.name { font-weight: 600; font-size: 14px; }
.ts { margin-left: auto; font-size: 11px; color: #6b7280; }
.summary { margin: 4px 0 8px; font-weight: 500; }
.desc { margin: 4px 0 12px; color: #4b5563; }
.value { margin: 4px 0; font-size: 12px; }
.value code { background: white; padding: 1px 6px; border-radius: 3px; border: 1px solid #e5e7eb; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.kv { width: 100%; border-collapse: collapse; margin: 8px 0; background: white; border: 1px solid #e5e7eb; border-radius: 4px; }
.kv caption { text-align: left; font-size: 11px; font-weight: 600; color: #6b7280; padding: 6px 8px; }
.kv th, .kv td { padding: 4px 8px; text-align: left; font-size: 12px; border-top: 1px solid #f3f4f6; }
.kv th { font-weight: 500; color: #6b7280; width: 30%; }
.kv td { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.hint { margin-top: 12px; font-size: 11px; color: #9ca3af; font-style: italic; }
`
