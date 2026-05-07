//go:build integration

package collector

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// TestSourceAttribution_PrometheusLabels verifies that OTel resource
// attributes (service.name, service.namespace) flow end-to-end from a
// third-party-style OTLP publisher through the embedded collector and
// into the Prometheus exporter as labels.
//
// Without resource_to_telemetry_conversion enabled, incoming metrics
// would land in a single unlabeled stream and the hub role (signals
// from third-party tools kept separately queryable) would be broken.
func TestSourceAttribution_PrometheusLabels(t *testing.T) {
	cfg := Config{
		GRPCPort:       allocFreePort(t),
		HTTPPort:       allocFreePort(t),
		PrometheusPort: allocFreePort(t),
	}
	c := NewEmbeddedCollector(cfg, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start collector: %v", err)
	}
	defer c.Stop(context.Background())

	waitForListen(t, fmt.Sprintf("127.0.0.1:%d", cfg.GRPCPort), 5*time.Second)

	const externalSvc = "external-tool"
	const externalNS = "fleet"
	exp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(fmt.Sprintf("127.0.0.1:%d", cfg.GRPCPort)),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("otlp exporter: %v", err)
	}
	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(externalSvc),
			semconv.ServiceNamespace(externalNS),
		),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(500*time.Millisecond))),
	)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	meter := mp.Meter("source-attribution-test")
	counter, err := meter.Int64Counter("ycode_source_attr_test_total")
	if err != nil {
		t.Fatalf("counter: %v", err)
	}
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", "smoke")))

	if err := mp.ForceFlush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}

	promURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", cfg.PrometheusPort)
	deadline := time.Now().Add(15 * time.Second)
	var lastSeen string
	for time.Now().Before(deadline) {
		body := scrape(promURL)
		lastSeen = body
		if strings.Contains(body, "ycode_source_attr_test_total") &&
			strings.Contains(body, `service_name="external-tool"`) &&
			strings.Contains(body, `service_namespace="fleet"`) {
			return // success
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("metric with service.name=external-tool label not observed; last excerpt:\n%s",
		excerpt(lastSeen, "ycode_source_attr_test_total"))
}

func waitForListen(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("listener at %s not ready within %v", addr, timeout)
}

func scrape(url string) string {
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func excerpt(body, needle string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	if len(body) > 600 {
		return body[:600] + "...(truncated)"
	}
	return body
}
