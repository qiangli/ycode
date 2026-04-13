package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qiangli/ycode/internal/bus"

	"go.opentelemetry.io/otel"
)

func TestOTELMiddleware(t *testing.T) {
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := New(Config{Token: "test-token"}, svc)

	// Setup OTEL with global tracer/meter.
	srv.SetOTEL(&OTELConfig{
		Tracer: otel.Tracer("test"),
		Meter:  otel.Meter("test"),
	})

	ts := httptest.NewServer(srv.otelMiddleware(srv.Mux()))
	t.Cleanup(ts.Close)

	// Make a request — should be instrumented.
	req, _ := http.NewRequest("GET", ts.URL+"/api/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Verify metrics were recorded (no panics, counters initialized).
	if srv.otelMetrics == nil {
		t.Error("otelMetrics should be initialized after SetOTEL")
	}
}

func TestOTELMiddleware_Disabled(t *testing.T) {
	memBus := bus.NewMemoryBus()
	t.Cleanup(func() { memBus.Close() })

	svc := &mockService{b: memBus}
	srv := New(Config{Token: "test-token"}, svc)

	// No OTEL setup — middleware should be a passthrough.
	ts := httptest.NewServer(srv.otelMiddleware(srv.Mux()))
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
