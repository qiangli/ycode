// Package collector manages the OpenTelemetry Collector as a subprocess.
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

	// Exporter ports.
	PrometheusPort int // where Prometheus scrapes metrics from
	HealthPort     int

	// VictoriaLogs endpoint for log/trace forwarding.
	VictoriaLogsPort int

	// Optional remote OTLP endpoint.
	RemoteOTLPEndpoint string
	RemoteOTLPHeaders  map[string]string
}

// GenerateYAML produces the collector config YAML from the given parameters.
func GenerateYAML(cfg Config) string {
	var b strings.Builder

	b.WriteString("receivers:\n")
	b.WriteString("  otlp:\n")
	b.WriteString("    protocols:\n")
	b.WriteString(fmt.Sprintf("      grpc: { endpoint: \"127.0.0.1:%d\" }\n", cfg.GRPCPort))
	b.WriteString(fmt.Sprintf("      http: { endpoint: \"127.0.0.1:%d\" }\n", cfg.HTTPPort))

	b.WriteString("\nprocessors:\n")
	b.WriteString("  batch:\n")
	b.WriteString("    timeout: 5s\n")

	b.WriteString("\nexporters:\n")
	b.WriteString(fmt.Sprintf("  prometheus:\n    endpoint: \"127.0.0.1:%d\"\n", cfg.PrometheusPort))

	if cfg.VictoriaLogsPort > 0 {
		b.WriteString(fmt.Sprintf("  otlphttp/vlogs:\n    endpoint: \"http://127.0.0.1:%d/insert/opentelemetry\"\n", cfg.VictoriaLogsPort))
	}

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
	b.WriteString("  pipelines:\n")

	traceExporters := []string{}
	logExporters := []string{}
	if cfg.VictoriaLogsPort > 0 {
		traceExporters = append(traceExporters, "otlphttp/vlogs")
		logExporters = append(logExporters, "otlphttp/vlogs")
	}
	if cfg.RemoteOTLPEndpoint != "" {
		traceExporters = append(traceExporters, "otlphttp/remote")
		logExporters = append(logExporters, "otlphttp/remote")
	}

	b.WriteString("    traces:\n")
	b.WriteString("      receivers: [otlp]\n")
	b.WriteString("      processors: [batch]\n")
	b.WriteString(fmt.Sprintf("      exporters: [%s]\n", strings.Join(traceExporters, ", ")))

	b.WriteString("    metrics:\n")
	b.WriteString("      receivers: [otlp]\n")
	b.WriteString("      processors: [batch]\n")
	b.WriteString("      exporters: [prometheus]\n")

	b.WriteString("    logs:\n")
	b.WriteString("      receivers: [otlp]\n")
	b.WriteString("      processors: [batch]\n")
	b.WriteString(fmt.Sprintf("      exporters: [%s]\n", strings.Join(logExporters, ", ")))

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
