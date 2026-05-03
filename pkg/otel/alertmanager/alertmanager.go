// Package alertmanager re-exports Prometheus Alertmanager types,
// isolating the rest of the codebase from the upstream dependency.
package alertmanager

import (
	amcluster "github.com/prometheus/alertmanager/cluster"
	amconfig "github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
	"github.com/prometheus/alertmanager/featurecontrol"
	"github.com/prometheus/alertmanager/provider/mem"
	"github.com/prometheus/alertmanager/silence"
	amtypes "github.com/prometheus/alertmanager/types"
	prometheus_model "github.com/prometheus/common/model"

	v2 "github.com/prometheus/alertmanager/api/v2"
	"github.com/prometheus/alertmanager/asset"
	"github.com/prometheus/client_golang/prometheus"
)

// Provider types.
type (
	Alerts    = mem.Alerts
	MemMarker = amtypes.MemMarker
	Alert     = amtypes.Alert
	AlertStatus = amtypes.AlertStatus
)

// Config types.
type (
	Config   = amconfig.Config
	Route    = amconfig.Route
	Receiver = amconfig.Receiver
)

// Dispatch types.
type (
	DispatchRoute  = dispatch.Route
	AlertGroups    = dispatch.AlertGroups
)

// Silence types.
type (
	SilenceOptions = silence.Options
)

// Cluster types.
type (
	ClusterMember = amcluster.ClusterMember
)

// Prometheus types.
type (
	Registry    = prometheus.Registry
	Fingerprint = prometheus_model.Fingerprint
	LabelSet    = prometheus_model.LabelSet
)

// Feature control.
type NoopFlags = featurecontrol.NoopFlags

// Alert state constants.
const AlertStateActive = amtypes.AlertStateActive

var (
	NewAlerts   = mem.NewAlerts
	NewMarker   = amtypes.NewMarker
	NewSilences = silence.New
	NewAPI      = v2.NewAPI
	NewRegistry = prometheus.NewRegistry
	Assets      = asset.Assets
)
