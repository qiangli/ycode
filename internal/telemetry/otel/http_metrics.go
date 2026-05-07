package otel

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	httpInstrumentsOnce sync.Once
	httpRequestTotal    metric.Int64Counter
	httpRequestDuration metric.Float64Histogram
	apiRetryAttempts    metric.Int64Counter
	apiRetryDelay       metric.Float64Histogram
	webFetchTotal       metric.Int64Counter
	webFetchDuration    metric.Float64Histogram
	webSearchTotal      metric.Int64Counter
	webSearchDuration   metric.Float64Histogram
	webSearchResults    metric.Int64Histogram
)

func ensureHTTPInstruments() {
	httpInstrumentsOnce.Do(func() {
		api := otel.Meter("ycode.api")
		httpRequestTotal, _ = api.Int64Counter(
			"ycode.http.request.total",
			metric.WithDescription("Outbound HTTP requests issued by ycode (LLM providers, web tools)"),
		)
		httpRequestDuration, _ = api.Float64Histogram(
			"ycode.http.request.duration",
			metric.WithUnit("ms"),
			metric.WithDescription("Outbound HTTP request latency"),
		)
		apiRetryAttempts, _ = api.Int64Counter(
			"ycode.api.retry.attempts",
			metric.WithDescription("Retry attempts for failed API calls"),
		)
		apiRetryDelay, _ = api.Float64Histogram(
			"ycode.api.retry.delay",
			metric.WithUnit("ms"),
			metric.WithDescription("Backoff delay between retry attempts"),
		)
		web := otel.Meter("ycode.web")
		webFetchTotal, _ = web.Int64Counter(
			"ycode.web.fetch.total",
			metric.WithDescription("Web fetch tool invocations"),
		)
		webFetchDuration, _ = web.Float64Histogram(
			"ycode.web.fetch.duration",
			metric.WithUnit("ms"),
			metric.WithDescription("Web fetch latency"),
		)
		webSearchTotal, _ = web.Int64Counter(
			"ycode.web.search.total",
			metric.WithDescription("Web search tool invocations"),
		)
		webSearchDuration, _ = web.Float64Histogram(
			"ycode.web.search.duration",
			metric.WithUnit("ms"),
			metric.WithDescription("Web search latency"),
		)
		webSearchResults, _ = web.Int64Histogram(
			"ycode.web.search.results",
			metric.WithDescription("Number of results returned per web search"),
		)
	})
}

// RecordHTTPRequest emits ycode.http.request.{total,duration} for one
// outbound HTTP call. Pass the request's method, the *server host*
// (not the full URL — paths and query strings would explode metric
// cardinality), the response status (0 if no response was received),
// the wall-clock duration, and a success flag.
func RecordHTTPRequest(ctx context.Context, method, host string, status int, dur time.Duration, success bool) {
	ensureHTTPInstruments()
	attrs := []attribute.KeyValue{
		attribute.String("http.method", method),
		attribute.String("http.host", host),
		attribute.Int("http.status_code", status),
		attribute.Bool("success", success),
	}
	if httpRequestTotal != nil {
		httpRequestTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if httpRequestDuration != nil {
		httpRequestDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attrs...))
	}
}

// RecordRetry emits ycode.api.retry.{attempts,delay} when the api/retry
// loop falls back to a retry attempt. `reason` is a coarse classification
// (e.g. "net_error", "5xx", "rate_limited") to keep cardinality bounded.
func RecordRetry(ctx context.Context, attempt int, delay time.Duration, reason string) {
	ensureHTTPInstruments()
	attrs := []attribute.KeyValue{
		attribute.Int("attempt", attempt),
		attribute.String("reason", reason),
	}
	if apiRetryAttempts != nil {
		apiRetryAttempts.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if apiRetryDelay != nil {
		apiRetryDelay.Record(ctx, float64(delay.Milliseconds()), metric.WithAttributes(attrs...))
	}
}

// RecordWebFetch emits ycode.web.fetch.{total,duration} for one web
// fetch tool invocation. `host` is the resolved request host; `bytes`
// is the body size after the read (or 0 if no body was read).
func RecordWebFetch(ctx context.Context, host string, status int, dur time.Duration, bytes int, success bool) {
	ensureHTTPInstruments()
	attrs := []attribute.KeyValue{
		attribute.String("host", host),
		attribute.Int("http.status_code", status),
		attribute.Bool("success", success),
	}
	if webFetchTotal != nil {
		webFetchTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if webFetchDuration != nil {
		webFetchDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attrs...))
	}
	_ = bytes // intentionally not exported as a separate metric — covered by tool.output.size attribute
}

// RecordWebSearch emits ycode.web.search.{total,duration,results} for
// one web search tool invocation. `provider` is the upstream search
// engine name (e.g. "searxng", "tavily").
func RecordWebSearch(ctx context.Context, provider string, dur time.Duration, results int, success bool) {
	ensureHTTPInstruments()
	attrs := []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.Bool("success", success),
	}
	if webSearchTotal != nil {
		webSearchTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if webSearchDuration != nil {
		webSearchDuration.Record(ctx, float64(dur.Milliseconds()), metric.WithAttributes(attrs...))
	}
	if webSearchResults != nil && results >= 0 {
		webSearchResults.Record(ctx, int64(results), metric.WithAttributes(attrs...))
	}
}
