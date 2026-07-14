package tools

import (
	"context"
	"strings"
	"testing"
)

// The behavior this whole change exists to guarantee: when the query tools fall back to
// ycode's own files, they SAY SO — loudly enough that an agent does not read an empty result
// as "nothing happened."
//
// A fallback that is silent is worse than no fallback: it answers a cross-service question
// with one process's data and gives no sign it did. That is the absence-of-evidence bug in the
// tool built to catch it.
func TestFallbackBannerWarnsThatLocalFilesAreNotTheWholePicture(t *testing.T) {
	// No store configured -> the source is local files, and the banner must warn.
	t.Setenv("BASHY_OTEL_QUERY_URL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	resetTelemetryStoreForTest()

	src := sourceOf(context.Background())
	if src.Store {
		t.Fatal("no store is configured, yet sourceOf reported the store as the source")
	}
	b := src.Banner()
	for _, must := range []string{"LOCAL FILES ONLY", "NOT HERE", "does not mean it did not happen"} {
		if !strings.Contains(b, must) {
			t.Errorf("fallback banner is missing the warning %q — an agent could read an empty\n"+
				"result as authoritative.\nbanner was:\n%s", must, b)
		}
	}
}

// The store must be PROBED, not assumed reachable from configuration alone. A configured
// endpoint with nothing listening is not a store; reporting it as one makes every empty
// answer a lie.
func TestUnreachableStoreFallsBackAndSaysWhy(t *testing.T) {
	// A port nothing listens on.
	t.Setenv("BASHY_OTEL_QUERY_URL", "http://127.0.0.1:1")
	resetTelemetryStoreForTest()

	src := sourceOf(context.Background())
	if src.Store {
		t.Fatal("an unreachable store was reported as the live source — configuration was mistaken for a connection")
	}
	if !strings.Contains(src.Detail, "not reachable") {
		t.Errorf("fallback did not explain WHY (want 'not reachable'); got %q", src.Detail)
	}
}
