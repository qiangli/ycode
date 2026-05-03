// Package collector re-exports OpenTelemetry Collector types and component factories,
// isolating the rest of the codebase from the upstream collector dependencies.
package collector

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/exporter/debugexporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/exporter/otlphttpexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor/batchprocessor"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"go.opentelemetry.io/collector/service/telemetry/otelconftelemetry"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/prometheusexporter"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/hostmetricsreceiver"
)

type (
	Collector              = otelcol.Collector
	CollectorSettings      = otelcol.CollectorSettings
	ConfigProviderSettings = otelcol.ConfigProviderSettings
	Factories              = otelcol.Factories
	BuildInfo              = component.BuildInfo
	Factory                = component.Factory
	Type                   = component.Type
	ResolverSettings       = confmap.ResolverSettings
	ProviderFactory        = confmap.ProviderFactory
)

// MakeFactoryMap wraps the generic otelcol.MakeFactoryMap.
func MakeFactoryMap[T Factory](factories ...T) (map[Type]T, error) {
	return otelcol.MakeFactoryMap(factories...)
}

var (
	NewCollector           = otelcol.NewCollector
	NewYAMLProviderFactory = yamlprovider.NewFactory

	// Receiver factories.
	NewOTLPReceiverFactory        = otlpreceiver.NewFactory
	NewHostMetricsReceiverFactory = hostmetricsreceiver.NewFactory

	// Processor factories.
	NewBatchProcessorFactory = batchprocessor.NewFactory

	// Exporter factories.
	NewDebugExporterFactory      = debugexporter.NewFactory
	NewPrometheusExporterFactory = prometheusexporter.NewFactory
	NewOTLPExporterFactory       = otlpexporter.NewFactory
	NewOTLPHTTPExporterFactory   = otlphttpexporter.NewFactory

	// Telemetry factory.
	NewTelemetryFactory = otelconftelemetry.NewFactory
)
