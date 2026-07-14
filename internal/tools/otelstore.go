package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/coreutils/pkg/otelquery"
)

// The telemetry store, and why the query tools now prefer it over their own files.
//
// ycode writes telemetry TWICE: local JSONL (which query_traces/query_logs read) and OTLP to
// the store (which they did not). So an agent asking "why was that slow" saw ONLY YCODE'S OWN
// SPANS — never bashy's per-command spans, never `cmd.exit_code`, never another service in the
// same trace.
//
// That is not a smaller answer. It is a MISLEADING one:
//
//	a JSONL file holds one PROCESS's spans.  A trace crosses processes.
//
// Ask a local file "what happened in this request" and you get back the part of the answer
// that happened to be written by the process you asked — with nothing to indicate the rest
// exists. The agent reads a complete-looking timeline and reasons about a fragment.
//
// # Provenance is the whole point of this file
//
// The store is preferred, and the local files remain as a fallback (local-first: an agent on a
// plane with no stack running still gets its own spans). But a fallback that does not ANNOUNCE
// ITSELF is worse than no fallback at all — the caller cannot tell
//
//	"the store says nothing happened"        (a fact)
//	"I could not reach the store, so I read  (not a fact about the system at all)
//	 my own files, which of course only
//	 contain me"
//
// apart, and an empty result reads as "nothing to see" in both cases. That is the
// absence-of-evidence bug, in the observability tool built to catch it.
//
// So EVERY result from these tools carries a source line. Always. Even when it found plenty.

// storeSource names where an answer came from, and is prepended to every query result.
type storeSource struct {
	// Store is true when the answer came from the telemetry store (all services, full traces).
	Store bool
	// Detail explains a fallback: why the store was not used.
	Detail string
	// URL is the store we tried.
	URL string
}

// Banner is the line the agent reads before the data. It is not decoration — it is the
// difference between a fact and a fragment.
func (s storeSource) Banner() string {
	if s.Store {
		return fmt.Sprintf("source: telemetry store (%s) — all services, complete traces", s.URL)
	}
	return fmt.Sprintf(
		"source: ycode's LOCAL FILES ONLY — %s\n"+
			"WARNING: these contain ycode's own spans and nothing else. Spans from bashy (including\n"+
			"every command's exit code), and any other service in the same trace, are NOT HERE. An\n"+
			"empty or short result does not mean it did not happen — it may mean nobody asked the\n"+
			"store. Start the stack (`bashy otel serve`) and set OTEL_EXPORTER_OTLP_ENDPOINT to see\n"+
			"the whole picture.", s.Detail)
}

var (
	storeOnce   sync.Once
	storeClient *otelquery.Client
	storeUp     bool
	storeWhy    string
)

// storeURL resolves where the telemetry store's query proxy lives.
//
// BASHY_OTEL_QUERY_URL is the explicit setting. Otherwise, if the process is exporting OTLP at
// all, the stack that receives it is by convention on the same host, and its query proxy is a
// known port — so a user who configured the standard OTEL variable gets querying for free
// rather than having to discover a second, bespoke one.
func storeURL() string {
	if u := strings.TrimSpace(os.Getenv("BASHY_OTEL_QUERY_URL")); u != "" {
		return u
	}
	if ep := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); ep != "" {
		return "http://127.0.0.1:31415" // the `bashy otel` reverse proxy
	}
	return ""
}

// telemetryStore returns the telemetry client, and whether it is actually reachable.
//
// Reachability is PROBED, not assumed. "The endpoint is configured" is not evidence that
// anything is listening on it, and a query layer that reports an empty result because it
// silently failed to connect is telling the agent something false.
func telemetryStore(ctx context.Context) (*otelquery.Client, bool, string) {
	storeOnce.Do(func() {
		u := storeURL()
		if u == "" {
			storeWhy = "no telemetry store configured (set OTEL_EXPORTER_OTLP_ENDPOINT or BASHY_OTEL_QUERY_URL)"
			return
		}
		c := otelquery.NewClient(u)
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if !c.Reachable(probeCtx) {
			storeWhy = fmt.Sprintf("telemetry store at %s is not reachable (is `bashy otel serve` running?)", u)
			return
		}
		storeClient, storeUp = c, true
	})
	return storeClient, storeUp, storeWhy
}

// sourceOf reports which backend a query will use, for the banner.
func sourceOf(ctx context.Context) storeSource {
	c, up, why := telemetryStore(ctx)
	if up {
		return storeSource{Store: true, URL: c.BaseURL}
	}
	return storeSource{Store: false, Detail: why}
}
