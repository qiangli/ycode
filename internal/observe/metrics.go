package observe

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Fleet metric names. They are deliberately fleet-scoped rather than
// ycode-scoped: the point of a per-turn record is to compare agents and models
// ACROSS tools, and a metric named after the emitter cannot be summed with its
// peers.
const (
	// MetricTokens counts billed tokens, split by kind (prompt/completion/
	// cache_read/cache_write).
	MetricTokens = "fleet.tokens"
	// MetricCost accumulates locally-priced spend in USD.
	MetricCost = "fleet.cost_usd"
	// MetricEscalation counts base→premium switches, keyed by the pair.
	MetricEscalation = "fleet.escalation"
)

// meterOnce lazily builds the instruments. A Recorder can outlive many turns
// and re-creating instruments per turn is pure waste; building them once, on
// first use, also means a process that never records a turn never touches OTEL
// at all.
//
// When no MeterProvider is configured (OTEL_EXPORTER_OTLP_ENDPOINT unset and no
// file exporter), the global provider is the SDK's no-op: every Add below
// compiles to a nil-ish call that allocates nothing and exports nothing. That
// is the "total no-op when unset" contract, enforced by construction rather
// than by an `if enabled` the caller has to remember.
var (
	meterOnce  sync.Once
	tokenCtr   metric.Int64Counter
	costCtr    metric.Float64Counter
	escalCtr   metric.Int64Counter
	meterReady bool
)

func fleetInstruments() bool {
	meterOnce.Do(func() {
		m := otel.Meter("ycode.fleet")
		var err error
		if tokenCtr, err = m.Int64Counter(MetricTokens,
			metric.WithDescription("Billed tokens per turn, by model/provider/kind")); err != nil {
			return
		}
		if costCtr, err = m.Float64Counter(MetricCost,
			metric.WithUnit("USD"),
			metric.WithDescription("Locally-priced spend per turn, by model/provider")); err != nil {
			return
		}
		if escalCtr, err = m.Int64Counter(MetricEscalation,
			metric.WithDescription("Cascade escalations from a base model to a premium one")); err != nil {
			return
		}
		meterReady = true
	})
	return meterReady
}

// recordMetrics publishes one flushed turn as fleet metrics. Called from the
// same place the JSONL line is written, so the two can never disagree.
//
// switched reports that THIS turn is the one where the served model changed.
// fleet.escalation counts switches, not escalated turns: a run that escalates
// once and then spends forty turns on the premium tier escalated ONCE, and a
// counter that says forty cannot answer "how often do cascades need help?".
func recordMetrics(ctx context.Context, rec *TurnRecord, switched bool) {
	if rec == nil || !fleetInstruments() {
		return
	}
	model := rec.Request.ServedModel
	provider := rec.Request.Provider
	base := attribute.NewSet(
		attribute.String("model", model),
		attribute.String("provider", provider),
	)

	for kind, n := range map[string]int{
		"prompt":      rec.Response.PromptTokens,
		"completion":  rec.Response.CompletionTokens,
		"cache_read":  rec.Response.CacheReadTokens,
		"cache_write": rec.Response.CacheWriteTokens,
	} {
		if n <= 0 {
			continue
		}
		tokenCtr.Add(ctx, int64(n), metric.WithAttributeSet(base),
			metric.WithAttributes(attribute.String("kind", kind)))
	}

	if rec.Response.CostUSD > 0 {
		costCtr.Add(ctx, rec.Response.CostUSD, metric.WithAttributeSet(base))
	}

	if switched {
		escalCtr.Add(ctx, 1, metric.WithAttributes(
			attribute.String("base_model", rec.Request.BaseModel),
			attribute.String("served_model", model),
			attribute.String("provider", provider),
			attribute.String("reason", rec.Request.Reason),
		))
	}
}
