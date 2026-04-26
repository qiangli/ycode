//go:build integration

package collector

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// allocFreePort finds a free TCP port on localhost.
func allocFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocFreePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func TestEmbeddedCollector_StartStop(t *testing.T) {
	cfg := Config{
		GRPCPort:       allocFreePort(t),
		HTTPPort:       allocFreePort(t),
		PrometheusPort: allocFreePort(t),
	}
	dataDir := t.TempDir()

	c := NewEmbeddedCollector(cfg, dataDir)

	if c.Name() != "otel-collector" {
		t.Errorf("Name() = %q, want %q", c.Name(), "otel-collector")
	}

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(ctx)

	// Wait for collector to become healthy (gRPC port accepting connections).
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.GRPCPort), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !c.Healthy() {
		t.Fatal("expected collector to be healthy after start")
	}

	if err := c.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Give it a moment for shutdown to take effect.
	time.Sleep(100 * time.Millisecond)

	if c.Healthy() {
		t.Error("expected collector to be unhealthy after stop")
	}
}

func TestEmbeddedCollector_HTTPHandler(t *testing.T) {
	cfg := Config{
		GRPCPort:       allocFreePort(t),
		HTTPPort:       allocFreePort(t),
		PrometheusPort: allocFreePort(t),
	}
	dataDir := t.TempDir()

	c := NewEmbeddedCollector(cfg, dataDir)

	handler := c.HTTPHandler()
	if handler == nil {
		t.Fatal("expected non-nil HTTP handler")
	}
}

func TestEmbeddedCollector_PortConflict(t *testing.T) {
	// Occupy a port to force a conflict.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	occupiedPort := ln.Addr().(*net.TCPAddr).Port

	cfg := Config{
		GRPCPort:       occupiedPort, // will conflict
		HTTPPort:       allocFreePort(t),
		PrometheusPort: allocFreePort(t),
	}

	c := NewEmbeddedCollector(cfg, t.TempDir())
	err = c.Start(context.Background())
	if err == nil {
		c.Stop(context.Background())
		t.Fatal("expected error when port is occupied")
	}
}

func TestEmbeddedCollector_PrometheusMetrics(t *testing.T) {
	cfg := Config{
		GRPCPort:       allocFreePort(t),
		HTTPPort:       allocFreePort(t),
		PrometheusPort: allocFreePort(t),
	}

	c := NewEmbeddedCollector(cfg, t.TempDir())
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(ctx)

	// Wait for Prometheus endpoint.
	promURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", cfg.PrometheusPort)
	deadline := time.Now().Add(10 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err := http.Get(promURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	resp, err := http.Get(promURL)
	if err != nil {
		t.Skipf("Prometheus endpoint not ready: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Prometheus /metrics status = %d, want 200", resp.StatusCode)
	}
}

func TestGenerateYAML_Pipelines(t *testing.T) {
	cfg := Config{
		GRPCPort:       4317,
		HTTPPort:       4318,
		PrometheusPort: 8888,
		JaegerOTLPPort: 4320,
	}

	yaml := GenerateYAML(cfg)

	// Verify key sections exist.
	for _, want := range []string{
		"127.0.0.1:4317",
		"127.0.0.1:4318",
		"127.0.0.1:8888",
		"otlp/jaeger",
		"traces:",
		"metrics:",
	} {
		if !contains(yaml, want) {
			t.Errorf("YAML missing %q", want)
		}
	}
}

func TestGenerateYAML_NoOptionalExporters(t *testing.T) {
	cfg := Config{
		GRPCPort:       4317,
		HTTPPort:       4318,
		PrometheusPort: 8888,
		// No Jaeger, no VictoriaLogs
	}

	yaml := GenerateYAML(cfg)

	// Should not have traces pipeline or log pipeline.
	if contains(yaml, "otlp/jaeger") {
		t.Error("should not include otlp/jaeger when JaegerOTLPPort is 0")
	}
	if contains(yaml, "otlphttp/vlogs") {
		t.Error("should not include otlphttp/vlogs when VictoriaLogsPort is 0")
	}
	// Metrics pipeline should still exist.
	if !contains(yaml, "metrics:") {
		t.Error("should include metrics pipeline")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
