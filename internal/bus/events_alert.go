package bus

import (
	"encoding/json"
	"time"
)

// AlertFiredPayload is the event data for an EventAlertFired event.
//
// Shape mirrors the standard Prometheus/Alertmanager alert envelope so
// payloads can be re-emitted to Alertmanager in parallel without
// translation. ycode self-healing consumers are expected to react to
// the labels (typically including `alertname`) rather than parse Summary.
type AlertFiredPayload struct {
	Name        string            `json:"name"`            // canonical alert name (= labels["alertname"])
	Severity    string            `json:"severity"`        // info / warning / critical (free-form)
	Summary     string            `json:"summary"`         // human-readable one-liner
	Description string            `json:"description"`     // longer human description
	Labels      map[string]string `json:"labels"`          // alert dimension labels
	Annotations map[string]string `json:"annotations"`     // free-form metadata
	StartsAt    time.Time         `json:"starts_at"`       // when the rule first tripped
	Value       float64           `json:"value,omitempty"` // sample value at firing time
	Source      string            `json:"source,omitempty"`
}

// PublishAlertFired emits an EventAlertFired event onto the bus. Safe to
// call with a nil bus (no-op), so call sites don't need to branch.
func PublishAlertFired(b Bus, p AlertFiredPayload) {
	if b == nil {
		return
	}
	if p.StartsAt.IsZero() {
		p.StartsAt = time.Now()
	}
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	b.Publish(Event{
		ID:        NextEventID(),
		Type:      EventAlertFired,
		Timestamp: time.Now(),
		Data:      data,
	})
}
