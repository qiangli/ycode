package eval

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds OTEL metric instruments for eval results.
type Metrics struct {
	compositeScore  metric.Float64Gauge
	passRate        metric.Float64Gauge
	passPowKMetric  metric.Float64Gauge
	flakinessMetric metric.Float64Gauge
	toolAccuracyM   metric.Float64Gauge
	trajectoryM     metric.Float64Gauge
	costUSD         metric.Float64Counter
	latencyMS       metric.Float64Histogram
	evalRunTotal    metric.Int64Counter
}

// NewMetrics registers eval-specific OTEL metric instruments.
func NewMetrics() (*Metrics, error) {
	meter := otel.Meter("ycode.eval")

	compositeScore, err := meter.Float64Gauge("ycode_eval_composite_score",
		metric.WithDescription("Composite eval score (0.0-1.0)"))
	if err != nil {
		return nil, err
	}

	passRate, err := meter.Float64Gauge("ycode_eval_pass_rate",
		metric.WithDescription("Pass@k rate per scenario (0.0-1.0)"))
	if err != nil {
		return nil, err
	}

	passPowK, err := meter.Float64Gauge("ycode_eval_pass_pow_k",
		metric.WithDescription("Pass^k reliability per scenario (0.0-1.0)"))
	if err != nil {
		return nil, err
	}

	flakiness, err := meter.Float64Gauge("ycode_eval_flakiness",
		metric.WithDescription("Flakiness score per scenario (0.0-1.0, lower is better)"))
	if err != nil {
		return nil, err
	}

	toolAcc, err := meter.Float64Gauge("ycode_eval_tool_accuracy",
		metric.WithDescription("Tool accuracy per scenario (0.0-1.0)"))
	if err != nil {
		return nil, err
	}

	traj, err := meter.Float64Gauge("ycode_eval_trajectory_score",
		metric.WithDescription("Trajectory similarity score per scenario (0.0-1.0)"))
	if err != nil {
		return nil, err
	}

	cost, err := meter.Float64Counter("ycode_eval_cost_usd",
		metric.WithDescription("Total eval cost in USD"))
	if err != nil {
		return nil, err
	}

	latency, err := meter.Float64Histogram("ycode_eval_latency_ms",
		metric.WithDescription("Eval scenario latency in milliseconds"))
	if err != nil {
		return nil, err
	}

	runs, err := meter.Int64Counter("ycode_eval_run_total",
		metric.WithDescription("Total number of eval runs"))
	if err != nil {
		return nil, err
	}

	return &Metrics{
		compositeScore:  compositeScore,
		passRate:        passRate,
		passPowKMetric:  passPowK,
		flakinessMetric: flakiness,
		toolAccuracyM:   toolAcc,
		trajectoryM:     traj,
		costUSD:         cost,
		latencyMS:       latency,
		evalRunTotal:    runs,
	}, nil
}

// RecordRun emits OTEL metrics for a complete eval run.
func (m *Metrics) RecordRun(ctx context.Context, report *Report) {
	attrs := []attribute.KeyValue{
		attribute.String("version", report.Version),
		attribute.String("provider", report.Provider),
		attribute.String("model", report.Model),
		attribute.String("tier", report.Tier),
	}
	attrSet := metric.WithAttributes(attrs...)

	m.compositeScore.Record(ctx, report.Composite, attrSet)
	m.evalRunTotal.Add(ctx, 1, attrSet)
	m.latencyMS.Record(ctx, float64(report.Duration.Milliseconds()), attrSet)

	for _, s := range report.Scenarios {
		scenarioAttrs := metric.WithAttributes(append(attrs,
			attribute.String("scenario", s.Scenario),
		)...)

		m.passRate.Record(ctx, s.Metrics.PassAtK, scenarioAttrs)
		m.passPowKMetric.Record(ctx, s.Metrics.PassPowK, scenarioAttrs)
		m.flakinessMetric.Record(ctx, s.Metrics.Flakiness, scenarioAttrs)

		if s.Metrics.ToolAccuracy > 0 {
			m.toolAccuracyM.Record(ctx, s.Metrics.ToolAccuracy, scenarioAttrs)
		}
		if s.Metrics.TrajectoryScore > 0 {
			m.trajectoryM.Record(ctx, s.Metrics.TrajectoryScore, scenarioAttrs)
		}
		if s.Metrics.MeanLatencyMS > 0 {
			m.latencyMS.Record(ctx, float64(s.Metrics.MeanLatencyMS), scenarioAttrs)
		}
	}
}
