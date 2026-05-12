package widget

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/a2ui"
)

// HealthSurfaceID is the well-known A2UI surface ID for the service-
// health overview that ycode publishes onto the canvas. Foreign agents
// discover it via the lighthouse manifest's `canvas.firstClassSurfaces`
// list and can `updateDataModel` against the same surfaceId to enrich
// the view (correlated services, runbook excerpts, etc.).
const HealthSurfaceID = "health"

// HealthHook publishes a first-class service-health A2UI surface onto a
// target session. The schema (component tree) is emitted once at start;
// the data model refreshes every RefreshInterval. New canvas subscribers
// see the surface within one refresh tick — replay isn't required.
//
// v1 ships with a small data shape (alert count, recent incidents,
// recent deploys, ycode version) drawn from sources the runtime can
// produce without an agent in the loop. The agent enriches the view at
// will by calling agent_render_a2ui against the same surface ID.
type HealthHook struct {
	bus             bus.Bus
	sessionID       string
	refreshInterval time.Duration
	dataProvider    HealthDataProvider
	logger          *slog.Logger
}

// HealthDataProvider returns the live data payload for the health
// surface. Implementations should be quick — this is called every
// refresh tick on the hot path. v1 callers can pass a closure that
// pulls AlertHook subscriptions / git head / runtime usage; v1.5
// callers can plug in agent-composed enrichment.
type HealthDataProvider func(ctx context.Context) HealthData

// HealthData is the JSON-marshalable payload pushed to the surface's
// data model on each refresh tick.
type HealthData struct {
	YcodeVersion string         `json:"ycodeVersion"`
	AlertsFiring int            `json:"alertsFiring"`
	Sessions     int            `json:"sessions"`
	Incidents    []HealthRow    `json:"incidents,omitempty"`
	Deploys      []HealthRow    `json:"deploys,omitempty"`
	Extras       map[string]any `json:"extras,omitempty"`
}

// HealthRow is the shape used for the incidents + deploys lists. Two
// labeled fields per item; the surface schema renders them as Code+Text
// pairs (deploys → sha+message) or Heading+Text (incidents → name+summary).
type HealthRow struct {
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"`
	Caption   string `json:"caption,omitempty"`
}

// DefaultRefreshInterval is the data-refresh cadence for the health
// surface. 30 seconds is brisk enough that a freshly-connected canvas
// observes the surface quickly while keeping render churn modest.
const DefaultRefreshInterval = 30 * time.Second

// NewHealthHook returns a hook that publishes the health surface onto
// the given session. Empty sessionID falls back to DefaultSession.
// dataProvider may be nil — the hook then publishes the schema only
// and an empty data model, useful for demo / smoke contexts.
func NewHealthHook(b bus.Bus, sessionID string, dataProvider HealthDataProvider) *HealthHook {
	if sessionID == "" {
		sessionID = DefaultSession
	}
	return &HealthHook{
		bus:             b,
		sessionID:       sessionID,
		refreshInterval: DefaultRefreshInterval,
		dataProvider:    dataProvider,
		logger:          slog.Default(),
	}
}

// SetRefreshInterval changes the cadence; must be called before Start.
func (h *HealthHook) SetRefreshInterval(d time.Duration) {
	if d > 0 {
		h.refreshInterval = d
	}
}

// Start emits the schema once and then refreshes the data model on the
// configured interval until ctx is done. Returns immediately after
// registering the goroutine.
func (h *HealthHook) Start(ctx context.Context) {
	if h.bus == nil {
		return
	}
	// Bootstrap: schema + initial data. Foreign agents reading the
	// manifest see the surface as available the moment Start is called.
	if err := h.publish(ctx, true); err != nil {
		h.logger.Warn("healthhook: bootstrap failed", "error", err)
	}
	go func() {
		t := time.NewTicker(h.refreshInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := h.publish(ctx, false); err != nil {
					h.logger.Warn("healthhook: refresh failed", "error", err)
				}
			}
		}
	}()
}

// publish builds an A2UI op batch (schema on bootstrap, data-only on
// refresh) and pushes it onto the bus as a single EventStateUpdate.
func (h *HealthHook) publish(ctx context.Context, includeSchema bool) error {
	var ops []a2ui.Op
	if includeSchema {
		ops = append(ops,
			a2ui.NewCreateSurface(HealthSurfaceID),
			a2ui.NewUpdateComponents(HealthSurfaceID, healthComponents()),
		)
	}
	// Data: snapshot whatever the provider gives us, default to a
	// minimal placeholder so the surface isn't blank on first paint.
	var data HealthData
	if h.dataProvider != nil {
		data = h.dataProvider(ctx)
	}
	ops = append(ops, a2ui.NewUpdateDataModel(HealthSurfaceID, "/", data))

	body, err := a2ui.Render(ops)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(a2uiPayload{
		Format: "a2ui",
		Body:   body,
		Origin: "ycode-health",
	})
	if err != nil {
		return err
	}
	h.bus.Publish(bus.Event{
		Type:      bus.EventStateUpdate,
		SessionID: h.sessionID,
		Timestamp: time.Now(),
		Data:      payload,
	})
	return nil
}

// healthComponents returns the static component tree for the health
// surface. The schema is small and stable — kept inline as JSON so the
// shape is auditable side-by-side with the canvas-side renderer.
func healthComponents() []json.RawMessage {
	const schema = `[
		{
			"id": "root",
			"component": "Column",
			"children": ["title", "kpi-row", "div-a", "incidents-heading", "incidents-list", "div-b", "deploys-heading", "deploys-list", "div-c", "footer"]
		},
		{ "id": "title", "component": "Heading", "level": 2, "text": "Service health" },
		{
			"id": "kpi-row",
			"component": "Row",
			"children": ["k-version", "k-alerts", "k-sessions"]
		},
		{ "id": "k-version",  "component": "Stat", "label": "Version",        "value": {"path": "/ycodeVersion"} },
		{ "id": "k-alerts",   "component": "Stat", "label": "Alerts firing",  "value": {"path": "/alertsFiring"} },
		{ "id": "k-sessions", "component": "Stat", "label": "Sessions",       "value": {"path": "/sessions"} },

		{ "id": "div-a", "component": "Divider" },

		{ "id": "incidents-heading", "component": "Heading", "level": 3, "text": "Open incidents" },
		{
			"id": "incidents-list",
			"component": "List",
			"direction": "vertical",
			"children": { "componentId": "incident-card", "path": "/incidents" }
		},
		{
			"id": "incident-card",
			"component": "Card",
			"child": "incident-card-col"
		},
		{
			"id": "incident-card-col",
			"component": "Column",
			"children": ["incident-name", "incident-summary"]
		},
		{ "id": "incident-name",    "component": "Heading", "level": 4, "text": {"path": "primary"} },
		{ "id": "incident-summary", "component": "Text",                 "text": {"path": "secondary"} },

		{ "id": "div-b", "component": "Divider" },

		{ "id": "deploys-heading", "component": "Heading", "level": 3, "text": "Recent deploys" },
		{
			"id": "deploys-list",
			"component": "List",
			"direction": "vertical",
			"children": { "componentId": "deploy-row", "path": "/deploys" }
		},
		{
			"id": "deploy-row",
			"component": "Row",
			"justify": "spaceBetween",
			"children": ["deploy-sha", "deploy-msg", "deploy-age"]
		},
		{ "id": "deploy-sha", "component": "Code",    "text": {"path": "primary"} },
		{ "id": "deploy-msg", "component": "Text",    "text": {"path": "secondary"} },
		{ "id": "deploy-age", "component": "Caption", "text": {"path": "caption"} },

		{ "id": "div-c", "component": "Divider" },

		{ "id": "footer", "component": "Caption", "text": "Refreshes every 30s · ask the agent to enrich this view" }
	]`
	var out []json.RawMessage
	if err := json.Unmarshal([]byte(schema), &out); err != nil {
		// Compile-time-ish guarantee: this schema is a literal in
		// source; a parse failure is a developer error, surface it
		// at startup rather than silently rendering nothing.
		panic("widget: invalid health schema: " + err.Error())
	}
	return out
}
