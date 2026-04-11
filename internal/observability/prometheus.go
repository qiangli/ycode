package observability

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrometheusConfig holds Prometheus configuration parameters.
type PrometheusConfig struct {
	Port             int
	CollectorMetrics int // port where collector exposes /metrics
	AlertmanagerPort int
	VictoriaLogsPort int
	DataDir          string // where TSDB data lives

	// Remote write targets.
	RemoteWrite []RemoteWriteTarget
	// Federation upstreams.
	Federation []FederationTarget
}

// RemoteWriteTarget for Prometheus remote-write.
type RemoteWriteTarget struct {
	URL       string
	Headers   map[string]string
	BasicAuth *BasicAuthConfig
}

// BasicAuthConfig for remote-write authentication.
type BasicAuthConfig struct {
	Username string
	Password string
}

// FederationTarget for Prometheus federation.
type FederationTarget struct {
	URL   string
	Match []string
}

// GeneratePrometheusConfig produces a prometheus.yml for the given parameters.
func GeneratePrometheusConfig(cfg PrometheusConfig) string {
	var b strings.Builder

	b.WriteString("global:\n")
	b.WriteString("  scrape_interval: 15s\n")
	b.WriteString("  evaluation_interval: 15s\n\n")

	// Alert rules.
	b.WriteString("rule_files:\n")
	b.WriteString("  - alerts/*.yml\n\n")

	// Alertmanager.
	if cfg.AlertmanagerPort > 0 {
		b.WriteString("alerting:\n")
		b.WriteString("  alertmanagers:\n")
		b.WriteString("    - static_configs:\n")
		b.WriteString(fmt.Sprintf("        - targets: ['127.0.0.1:%d']\n\n", cfg.AlertmanagerPort))
	}

	// Scrape configs.
	b.WriteString("scrape_configs:\n")

	// Self.
	b.WriteString("  - job_name: 'prometheus'\n")
	b.WriteString(fmt.Sprintf("    static_configs:\n      - targets: ['127.0.0.1:%d']\n\n", cfg.Port))

	// OTEL Collector metrics.
	if cfg.CollectorMetrics > 0 {
		b.WriteString("  - job_name: 'otel-collector'\n")
		b.WriteString(fmt.Sprintf("    static_configs:\n      - targets: ['127.0.0.1:%d']\n\n", cfg.CollectorMetrics))
	}

	// VictoriaLogs metrics.
	if cfg.VictoriaLogsPort > 0 {
		b.WriteString("  - job_name: 'victoria-logs'\n")
		b.WriteString(fmt.Sprintf("    static_configs:\n      - targets: ['127.0.0.1:%d']\n\n", cfg.VictoriaLogsPort))
	}

	// Remote write.
	if len(cfg.RemoteWrite) > 0 {
		b.WriteString("remote_write:\n")
		for _, rw := range cfg.RemoteWrite {
			b.WriteString(fmt.Sprintf("  - url: \"%s\"\n", rw.URL))
			if len(rw.Headers) > 0 {
				b.WriteString("    headers:\n")
				for k, v := range rw.Headers {
					b.WriteString(fmt.Sprintf("      %s: \"%s\"\n", k, v))
				}
			}
			if rw.BasicAuth != nil {
				b.WriteString("    basic_auth:\n")
				b.WriteString(fmt.Sprintf("      username: \"%s\"\n", rw.BasicAuth.Username))
				b.WriteString(fmt.Sprintf("      password: \"%s\"\n", rw.BasicAuth.Password))
			}
		}
		b.WriteString("\n")
	}

	// Federation.
	if len(cfg.Federation) > 0 {
		for i, fed := range cfg.Federation {
			b.WriteString(fmt.Sprintf("  - job_name: 'federate-%d'\n", i))
			b.WriteString("    honor_labels: true\n")
			b.WriteString("    metrics_path: '/federate'\n")
			b.WriteString("    params:\n      'match[]':\n")
			for _, m := range fed.Match {
				b.WriteString(fmt.Sprintf("        - '%s'\n", m))
			}
			b.WriteString(fmt.Sprintf("    static_configs:\n      - targets: ['%s']\n\n", fed.URL))
		}
	}

	return b.String()
}

// WritePrometheusConfig writes prometheus.yml to the given directory.
func WritePrometheusConfig(dir string, cfg PrometheusConfig) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Create alerts subdirectory.
	_ = os.MkdirAll(filepath.Join(dir, "alerts"), 0o755)

	path := filepath.Join(dir, "config.yml")
	data := GeneratePrometheusConfig(cfg)
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
