// Package collector manages the OpenTelemetry Collector.
package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds the collector configuration parameters.
type Config struct {
	// Receiver ports (allocated dynamically).
	GRPCPort int
	HTTPPort int

	// Exporter targets.
	PrometheusPort         int    // where Prometheus scrapes metrics from
	VictoriaLogsPort       int    // VictoriaLogs OTLP HTTP endpoint for logs
	VictoriaLogsPathPrefix string // path prefix VictoriaLogs requires (e.g. "/logs")
	JaegerOTLPPort         int    // Jaeger OTLP gRPC endpoint for traces
	HealthPort             int

	// Optional remote OTLP endpoint.
	RemoteOTLPEndpoint string
	RemoteOTLPHeaders  map[string]string
}

// GenerateYAML produces the collector config YAML from the given parameters.
// Pipeline routing: metrics→Prometheus, logs→VictoriaLogs, traces→Jaeger.
func GenerateYAML(cfg Config) string {
	var b strings.Builder

	b.WriteString("receivers:\n")
	b.WriteString("  otlp:\n")
	b.WriteString("    protocols:\n")
	b.WriteString(fmt.Sprintf("      grpc: { endpoint: \"127.0.0.1:%d\" }\n", cfg.GRPCPort))
	b.WriteString(fmt.Sprintf("      http: { endpoint: \"127.0.0.1:%d\" }\n", cfg.HTTPPort))

	// Host metrics receiver — collects CPU, memory, disk, network, filesystem, load, and process metrics.
	b.WriteString("  hostmetrics:\n")
	b.WriteString("    collection_interval: 15s\n")
	b.WriteString("    scrapers:\n")
	b.WriteString("      cpu:\n")
	b.WriteString("      memory:\n")
	b.WriteString("      disk:\n")
	b.WriteString("      network:\n")
	b.WriteString("      filesystem:\n")
	b.WriteString("      load:\n")
	b.WriteString("      paging:\n")
	b.WriteString("      processes:\n")

	b.WriteString("\nprocessors:\n")
	b.WriteString("  batch:\n")
	b.WriteString("    timeout: 5s\n")

	b.WriteString("\nexporters:\n")

	// Metrics → Prometheus
	b.WriteString(fmt.Sprintf("  prometheus:\n    endpoint: \"127.0.0.1:%d\"\n", cfg.PrometheusPort))

	// Logs → VictoriaLogs (OTLP HTTP)
	// The endpoint must include the http.pathPrefix that VictoriaLogs requires
	// (set by the stack manager for reverse-proxy routing). Without the prefix,
	// VictoriaLogs rejects requests with "missing -http.pathPrefix" error.
	if cfg.VictoriaLogsPort > 0 {
		prefix := strings.TrimSuffix(cfg.VictoriaLogsPathPrefix, "/")
		b.WriteString(fmt.Sprintf("  otlphttp/vlogs:\n    endpoint: \"http://127.0.0.1:%d%s/insert/opentelemetry\"\n", cfg.VictoriaLogsPort, prefix))
	}

	// Traces → Jaeger (OTLP gRPC — Jaeger v2 natively accepts OTLP)
	if cfg.JaegerOTLPPort > 0 {
		b.WriteString(fmt.Sprintf("  otlp/jaeger:\n    endpoint: \"127.0.0.1:%d\"\n    tls:\n      insecure: true\n", cfg.JaegerOTLPPort))
	}

	// Optional remote OTLP endpoint (receives all signals)
	if cfg.RemoteOTLPEndpoint != "" {
		b.WriteString(fmt.Sprintf("  otlphttp/remote:\n    endpoint: \"%s\"\n", cfg.RemoteOTLPEndpoint))
		if len(cfg.RemoteOTLPHeaders) > 0 {
			b.WriteString("    headers:\n")
			for k, v := range cfg.RemoteOTLPHeaders {
				b.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, v))
			}
		}
	}

	// Pipelines.
	b.WriteString("\nservice:\n")
	b.WriteString("  telemetry:\n")
	b.WriteString("    logs:\n")
	b.WriteString("      level: error\n")
	b.WriteString("    metrics:\n")
	b.WriteString("      level: none\n")
	b.WriteString("  pipelines:\n")

	// Traces → Jaeger (+ optional remote)
	traceExporters := []string{}
	if cfg.JaegerOTLPPort > 0 {
		traceExporters = append(traceExporters, "otlp/jaeger")
	}
	if cfg.RemoteOTLPEndpoint != "" {
		traceExporters = append(traceExporters, "otlphttp/remote")
	}
	if len(traceExporters) > 0 {
		b.WriteString("    traces:\n")
		b.WriteString("      receivers: [otlp]\n")
		b.WriteString("      processors: [batch]\n")
		b.WriteString(fmt.Sprintf("      exporters: [%s]\n", strings.Join(traceExporters, ", ")))
	}

	// Metrics → Prometheus (includes both OTLP app metrics and host metrics)
	b.WriteString("    metrics:\n")
	b.WriteString("      receivers: [otlp, hostmetrics]\n")
	b.WriteString("      processors: [batch]\n")
	b.WriteString("      exporters: [prometheus]\n")

	// Logs → VictoriaLogs (+ optional remote)
	logExporters := []string{}
	if cfg.VictoriaLogsPort > 0 {
		logExporters = append(logExporters, "otlphttp/vlogs")
	}
	if cfg.RemoteOTLPEndpoint != "" {
		logExporters = append(logExporters, "otlphttp/remote")
	}
	if len(logExporters) > 0 {
		b.WriteString("    logs:\n")
		b.WriteString("      receivers: [otlp]\n")
		b.WriteString("      processors: [batch]\n")
		b.WriteString(fmt.Sprintf("      exporters: [%s]\n", strings.Join(logExporters, ", ")))
	}

	return b.String()
}

// WriteConfig writes the collector config YAML to the given directory.
func WriteConfig(dir string, cfg Config) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create collector dir: %w", err)
	}
	path := filepath.Join(dir, "config.yaml")
	data := GenerateYAML(cfg)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return "", fmt.Errorf("write collector config: %w", err)
	}
	return path, nil
}
