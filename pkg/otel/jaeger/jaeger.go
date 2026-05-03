// Package jaeger re-exports Jaeger embedded collector types,
// isolating the rest of the codebase from the upstream dependency.
package jaeger

import (
	jaegerembed "github.com/jaegertracing/jaeger/cmd/jaeger/embed"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	provider "go.opentelemetry.io/collector/confmap/provider/yamlprovider"
	"go.opentelemetry.io/collector/otelcol"
)

type (
	Collector                = otelcol.Collector
	CollectorSettings        = otelcol.CollectorSettings
	ConfigProviderSettings   = otelcol.ConfigProviderSettings
	BuildInfo                = component.BuildInfo
	ResolverSettings         = confmap.ResolverSettings
	ProviderFactory          = confmap.ProviderFactory
)

var (
	Components          = jaegerembed.Components
	NewCollector        = otelcol.NewCollector
	NewYAMLProviderFactory = provider.NewFactory
)
