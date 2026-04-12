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

// AlertmanagerComponent provides an embedded alert manager running as a goroutine.
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
	Status      string            `json:"status"`
	Fingerprint string            `json:"fingerprint,omitempty"`
}

func NewAlertmanagerComponent() *AlertmanagerComponent { return &AlertmanagerComponent{} }
func (a *AlertmanagerComponent) Name() string          { return "alertmanager" }

func (a *AlertmanagerComponent) Start(ctx context.Context) error {
	cleanupCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	go a.cleanupLoop(cleanupCtx)
	a.healthy.Store(true)
	slog.Info("alertmanager: started")
	return nil
}

func (a *AlertmanagerComponent) Stop(_ context.Context) error {
	a.healthy.Store(false)
	if a.cancel != nil {
		a.cancel()
	}
	slog.Info("alertmanager: stopped")
	return nil
}

func (a *AlertmanagerComponent) Healthy() bool { return a.healthy.Load() }

func (a *AlertmanagerComponent) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/alerts", a.handleAlerts)
	mux.HandleFunc("/api/v1/alerts", a.handleAlerts)
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "OK") })
	mux.HandleFunc("/", a.handleUI)
	return mux
}

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
		fp := ""
		for k, v := range alert.Labels {
			fp += k + "=" + v + ","
		}
		alert.Fingerprint = fp
	}
	for i, e := range a.alerts {
		if e.Fingerprint == alert.Fingerprint {
			a.alerts[i] = alert
			return
		}
	}
	a.alerts = append(a.alerts, alert)
}

func (a *AlertmanagerComponent) Alerts() []Alert {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := make([]Alert, len(a.alerts))
	copy(r, a.alerts)
	return r
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
			for _, al := range a.alerts {
				if al.Status == "firing" || now.Sub(al.EndsAt) < 5*time.Minute {
					alive = append(alive, al)
				}
			}
			a.alerts = alive
			a.mu.Unlock()
		}
	}
}

func (a *AlertmanagerComponent) handleAlerts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a.Alerts())
	case http.MethodPost:
		var alerts []Alert
		if err := json.NewDecoder(r.Body).Decode(&alerts); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for _, al := range alerts {
			a.AddAlert(al)
		}
		fmt.Fprint(w, `{"status":"success"}`)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *AlertmanagerComponent) handleUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	alerts := a.Alerts()
	fmt.Fprint(w, `<!DOCTYPE html><html><head><title>ycode Alerts</title>
<style>body{font-family:sans-serif;max-width:1000px;margin:20px auto;padding:0 20px}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:4px 8px;border-bottom:1px solid #ddd}
.firing{color:#d32f2f}.resolved{color:#388e3c}</style></head><body><h2>ycode Alerts</h2>`)
	if len(alerts) == 0 {
		fmt.Fprint(w, "<p>No active alerts.</p>")
	} else {
		fmt.Fprint(w, `<table><thead><tr><th>Status</th><th>Alert</th><th>Started</th></tr></thead><tbody>`)
		for _, al := range alerts {
			cls := "resolved"
			if al.Status == "firing" {
				cls = "firing"
			}
			fmt.Fprintf(w, `<tr><td class="%s">%s</td><td>%s</td><td>%s</td></tr>`,
				cls, al.Status, al.Labels["alertname"], al.StartsAt.Format(time.RFC3339))
		}
		fmt.Fprint(w, "</tbody></table>")
	}
	fmt.Fprint(w, "</body></html>")
}
