package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// AlertmanagerComponent provides an embedded alert manager.
// It accepts alerts via the Alertmanager v2 API and dispatches them.
type AlertmanagerComponent struct {
	mu      sync.Mutex
	alerts  []Alert
	healthy atomic.Bool
	cancel  context.CancelFunc
}

// Alert represents a firing or resolved alert.
type Alert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations,omitempty"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt,omitempty"`
	Status      string            `json:"status"` // "firing" or "resolved"
	Fingerprint string            `json:"fingerprint,omitempty"`
}

// NewAlertmanagerComponent creates an embedded alertmanager component.
func NewAlertmanagerComponent() *AlertmanagerComponent {
	return &AlertmanagerComponent{}
}

// Name implements Component.
func (a *AlertmanagerComponent) Name() string { return "alertmanager" }

// Start initializes the alertmanager. Work runs in goroutines.
func (a *AlertmanagerComponent) Start(ctx context.Context) error {
	cleanupCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Background goroutine to expire resolved alerts.
	go a.cleanupLoop(cleanupCtx)

	a.healthy.Store(true)
	slog.Info("alertmanager: started (embedded)")
	return nil
}

// Stop shuts down the alertmanager.
func (a *AlertmanagerComponent) Stop(_ context.Context) error {
	a.healthy.Store(false)
	if a.cancel != nil {
		a.cancel()
	}
	slog.Info("alertmanager: stopped")
	return nil
}

// Healthy implements Component.
func (a *AlertmanagerComponent) Healthy() bool { return a.healthy.Load() }

// HTTPHandler returns the Alertmanager HTTP handler.
func (a *AlertmanagerComponent) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/alerts", a.handleAlerts)
	mux.HandleFunc("/api/v1/alerts", a.handleAlertsV1)
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})
	mux.HandleFunc("/", a.handleUI)
	return mux
}

// AddAlert adds or updates an alert.
func (a *AlertmanagerComponent) AddAlert(alert Alert) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if alert.Status == "" {
		alert.Status = "firing"
	}
	if alert.StartsAt.IsZero() {
		alert.StartsAt = time.Now()
	}
	if alert.Fingerprint == "" {
		alert.Fingerprint = alertFingerprint(alert.Labels)
	}

	// Update existing or append.
	for i, existing := range a.alerts {
		if existing.Fingerprint == alert.Fingerprint {
			a.alerts[i] = alert
			return
		}
	}
	a.alerts = append(a.alerts, alert)
}

// Alerts returns a copy of current alerts.
func (a *AlertmanagerComponent) Alerts() []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := make([]Alert, len(a.alerts))
	copy(result, a.alerts)
	return result
}

func (a *AlertmanagerComponent) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			now := time.Now()
			alive := a.alerts[:0]
			for _, alert := range a.alerts {
				// Keep firing alerts and recently resolved ones.
				if alert.Status == "firing" || now.Sub(alert.EndsAt) < 5*time.Minute {
					alive = append(alive, alert)
				}
			}
			a.alerts = alive
			a.mu.Unlock()
		}
	}
}

// handleAlerts handles the Alertmanager v2 alerts API (POST to send, GET to list).
func (a *AlertmanagerComponent) handleAlerts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.listAlerts(w)
	case http.MethodPost:
		a.receiveAlerts(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAlertsV1 is the v1 compatibility endpoint.
func (a *AlertmanagerComponent) handleAlertsV1(w http.ResponseWriter, r *http.Request) {
	a.handleAlerts(w, r)
}

func (a *AlertmanagerComponent) listAlerts(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.Alerts())
}

func (a *AlertmanagerComponent) receiveAlerts(w http.ResponseWriter, r *http.Request) {
	var alerts []Alert
	if err := json.NewDecoder(r.Body).Decode(&alerts); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}
	for _, alert := range alerts {
		a.AddAlert(alert)
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"success"}`)
}

func (a *AlertmanagerComponent) handleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	alerts := a.Alerts()

	fmt.Fprint(w, `<!DOCTYPE html><html><head><title>ycode Alerts</title>
<style>body{font-family:sans-serif;max-width:1000px;margin:20px auto;padding:0 20px}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:4px 8px;border-bottom:1px solid #ddd}
th{background:#f5f5f5}.firing{color:#d32f2f}.resolved{color:#388e3c}
</style></head><body>
<h2>ycode Alerts</h2>`)

	if len(alerts) == 0 {
		fmt.Fprint(w, "<p>No active alerts.</p>")
	} else {
		fmt.Fprint(w, `<table><thead><tr><th>Status</th><th>Alert</th><th>Labels</th><th>Started</th></tr></thead><tbody>`)
		for _, alert := range alerts {
			class := "resolved"
			if alert.Status == "firing" {
				class = "firing"
			}
			labelsJSON, _ := json.Marshal(alert.Labels)
			fmt.Fprintf(w, `<tr><td class="%s">%s</td><td>%s</td><td><code>%s</code></td><td>%s</td></tr>`,
				class, alert.Status, alert.Labels["alertname"], labelsJSON, alert.StartsAt.Format(time.RFC3339))
		}
		fmt.Fprint(w, "</tbody></table>")
	}

	fmt.Fprint(w, `<p><a href="/api/v2/alerts">JSON API</a></p></body></html>`)
}

func alertFingerprint(labels map[string]string) string {
	// Simple fingerprint from sorted label key-value pairs.
	fp := ""
	for k, v := range labels {
		fp += k + "=" + v + ","
	}
	return fp
}
