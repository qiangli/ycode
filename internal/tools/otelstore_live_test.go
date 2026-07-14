package tools

import (
	"context"
	"os"
	"testing"
)

// A real end-to-end check against a live telemetry store, skipped unless one is configured.
//
// Every OTHER test in this area mocks the HTTP server — which is exactly how the store schema
// went unnoticed: a mock returns whatever flat rows you hand it, so it can never reveal that
// the real VictoriaTraces prefixes attributes (span_attr:, resource_attr:, event:event_attr:).
// This test talks to the actual store, so it CAN.
//
//	BASHY_OTEL_QUERY_URL=http://127.0.0.1:31415 go test -run TestLiveStore ./internal/tools
func TestLiveStoreSpansCarryStrippedAttributes(t *testing.T) {
	if os.Getenv("BASHY_OTEL_QUERY_URL") == "" {
		t.Skip("no live store (set BASHY_OTEL_QUERY_URL to run)")
	}
	resetTelemetryStoreForTest()

	c, up, why := telemetryStore(context.Background())
	if !up {
		t.Fatalf("store configured but not reachable: %s", why)
	}

	spans, err := storeSpans(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) == 0 {
		t.Skip("store reachable but empty — run some telemetry-emitting commands first")
	}

	// The load-bearing assertion: an exec span's cmd.exit_code must be reachable by its BARE
	// name, even though the store stores it as span_attr:cmd.exit_code. If prefix-stripping
	// regressed, this reads empty and every failed-command query silently finds nothing.
	var sawExec, sawExit bool
	for _, s := range spans {
		if s.attrString("cmd.name") != "" {
			sawExec = true
			if s.attrString("cmd.exit_code") != "" {
				sawExit = true
			}
		}
	}
	if sawExec && !sawExit {
		t.Error("found exec spans but none exposed cmd.exit_code by its bare name — the " +
			"span_attr: prefix leaked, and failed-command queries will silently return nothing")
	}
	t.Logf("live store: %d spans, from %s", len(spans), c.BaseURL)
}
