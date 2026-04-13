package observability

import (
	"context"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"time"

	amcluster "github.com/prometheus/alertmanager/cluster"
	amconfig "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
	"github.com/prometheus/alertmanager/featurecontrol"
	"github.com/prometheus/alertmanager/provider/mem"
	"github.com/prometheus/alertmanager/silence"
	"github.com/prometheus/alertmanager/types"
	prometheus_model "github.com/prometheus/common/model"

	v2 "github.com/prometheus/alertmanager/api/v2"
	"github.com/prometheus/alertmanager/asset"
	"github.com/prometheus/client_golang/prometheus"
)

// AlertmanagerComponent runs an embedded Alertmanager with the real upstream
// API v2 and the Elm-based UI from the alertmanager/asset package.
type AlertmanagerComponent struct {
	alerts  *mem.Alerts
	marker  *types.MemMarker
	healthy atomic.Bool
	cancel  context.CancelFunc
}

func NewAlertmanagerComponent() *AlertmanagerComponent { return &AlertmanagerComponent{} }
func (a *AlertmanagerComponent) Name() string          { return "alertmanager" }

func (a *AlertmanagerComponent) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	reg := prometheus.NewRegistry()
	logger := slog.Default()

	a.marker = types.NewMarker(reg)

	alerts, err := mem.NewAlerts(ctx, a.marker, 30*time.Minute, 0, nil, logger, reg, featurecontrol.NoopFlags{})
	if err != nil {
		return err
	}
	a.alerts = alerts

	a.healthy.Store(true)
	slog.Info("alertmanager: started")
	return nil
}

func (a *AlertmanagerComponent) Stop(_ context.Context) error {
	a.healthy.Store(false)
	if a.cancel != nil {
		a.cancel()
	}
	if a.alerts != nil {
		a.alerts.Close()
	}
	slog.Info("alertmanager: stopped")
	return nil
}

func (a *AlertmanagerComponent) Healthy() bool { return a.healthy.Load() }

func (a *AlertmanagerComponent) HTTPHandler() http.Handler {
	reg := prometheus.NewRegistry()
	logger := slog.Default()

	silences, err := silence.New(silence.Options{
		Retention: 24 * time.Hour,
		Logger:    logger,
		Metrics:   reg,
	})
	if err != nil {
		slog.Warn("alertmanager: silence init failed, using stub", "error", err)
		return a.fallbackHandler()
	}

	peer := &noopPeer{}

	// groupsFn returns alert groups — empty since we have no dispatcher.
	groupsFn := func(_ context.Context, _ func(*dispatch.Route) bool, _ func(*types.Alert, time.Time) bool) (dispatch.AlertGroups, map[prometheus_model.Fingerprint][]string, error) {
		return nil, nil, nil
	}
	// getAlertStatusFn returns the status for a given alert fingerprint.
	getAlertStatusFn := func(_ prometheus_model.Fingerprint) types.AlertStatus {
		return types.AlertStatus{State: types.AlertStateActive}
	}
	// groupMutedFunc returns the muted state.
	groupMutedFunc := func(routeID, groupKey string) ([]string, bool) {
		return a.marker.Muted(routeID, groupKey)
	}

	api, err := v2.NewAPI(a.alerts, groupsFn, getAlertStatusFn, groupMutedFunc, silences, peer, logger, reg)
	if err != nil {
		slog.Warn("alertmanager: API v2 init failed, using stub", "error", err)
		return a.fallbackHandler()
	}

	// Initialize with a minimal config so the status endpoint works.
	defaultReceiver := "default"
	api.Update(&amconfig.Config{
		Route:     &amconfig.Route{Receiver: defaultReceiver},
		Receivers: []amconfig.Receiver{{Name: defaultReceiver}},
	}, func(_ context.Context, _ prometheus_model.LabelSet) {})

	mux := http.NewServeMux()
	// Mount the real API v2.
	mux.Handle("/api/v2/", api.Handler)
	// Health/ready endpoints.
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("OK")) })
	mux.HandleFunc("/-/ready", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("OK")) })

	// Serve the real Alertmanager Elm UI from embedded assets.
	fs := http.FileServer(asset.Assets)
	mux.HandleFunc("/script.js", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/static/script.js"
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/static/favicon.ico"
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/lib/", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = path.Join("/static/lib", strings.TrimPrefix(r.URL.Path, "/lib"))
		fs.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/static/"
		fs.ServeHTTP(w, r)
	})

	return mux
}

// fallbackHandler returns a minimal handler if the real API fails to initialize.
func (a *AlertmanagerComponent) fallbackHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/-/healthy", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("OK")) })
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Alertmanager UI unavailable"))
	})
	return mux
}

// AddAlert inserts an alert via the in-memory provider.
func (a *AlertmanagerComponent) AddAlert(ctx context.Context, alert *types.Alert) error {
	if a.alerts == nil {
		return nil
	}
	return a.alerts.Put(ctx, alert)
}

// noopPeer implements cluster.ClusterPeer for single-node operation.
type noopPeer struct{}

func (n *noopPeer) Name() string                     { return "embedded" }
func (n *noopPeer) Status() string                   { return "disabled" }
func (n *noopPeer) Peers() []amcluster.ClusterMember { return nil }
