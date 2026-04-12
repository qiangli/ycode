package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/alertmanager/asset"
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

	// Serve the real Alertmanager web UI from embedded assets.
	fs := http.FileServer(asset.Assets)
	mux.HandleFunc("/script.js", func(w http.ResponseWriter, r *http.Request) {
		disableAlertCaching(w)
		r.URL.Path = "/static/script.js"
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		disableAlertCaching(w)
		r.URL.Path = "/static/favicon.ico"
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/lib/", func(w http.ResponseWriter, r *http.Request) {
		disableAlertCaching(w)
		// Map /lib/foo → /static/lib/foo
		r.URL.Path = path.Join("/static/lib", strings.TrimPrefix(r.URL.Path, "/lib"))
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		disableAlertCaching(w)
		r.URL.Path = "/static/"
		fs.ServeHTTP(w, r)
	})
	return mux
}

func disableAlertCaching(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
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
