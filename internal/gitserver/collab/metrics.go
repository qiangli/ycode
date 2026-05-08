package collab

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds the OTEL instruments used by the orchestrator.
// Acquired once per Orchestrator lifetime via newMetrics.
type Metrics struct {
	iterations metric.Int64Counter
	prs        metric.Int64Counter
	ciRuns     metric.Int64Counter
	queueDepth metric.Int64Gauge
}

var (
	metricsOnce  sync.Once
	cachedMeters *Metrics
	cachedErr    error
)

// newMetrics constructs (or returns the cached) Metrics. The instruments
// themselves are global to the process — caching avoids re-registering
// them per Orchestrator instance.
func newMetrics() (*Metrics, error) {
	metricsOnce.Do(func() {
		m := otel.Meter("ycode.collab")
		var err error
		var ic metric.Int64Counter
		ic, err = m.Int64Counter("ycode_agent_iterations_total",
			metric.WithDescription("Total agent loop iterations across all projects"))
		if err != nil {
			cachedErr = err
			return
		}
		var prc metric.Int64Counter
		prc, err = m.Int64Counter("ycode_agent_pr_total",
			metric.WithDescription("Total PR outcomes by status (merged/conflict/abandoned)"))
		if err != nil {
			cachedErr = err
			return
		}
		var ci metric.Int64Counter
		ci, err = m.Int64Counter("ycode_agent_ci_runs_total",
			metric.WithDescription("Total CI runs by result (pass/fail)"))
		if err != nil {
			cachedErr = err
			return
		}
		var qd metric.Int64Gauge
		qd, err = m.Int64Gauge("ycode_tasks_queue_depth",
			metric.WithDescription("Open issues by priority"))
		if err != nil {
			cachedErr = err
			return
		}
		cachedMeters = &Metrics{
			iterations: ic,
			prs:        prc,
			ciRuns:     ci,
			queueDepth: qd,
		}
	})
	return cachedMeters, cachedErr
}

// recordIteration is called once per Pop→work→push cycle.
func (m *Metrics) recordIteration(ctx context.Context, agentID, projectSlug string) {
	if m == nil || m.iterations == nil {
		return
	}
	m.iterations.Add(ctx, 1, metric.WithAttributes(
		attribute.String("agent.id", agentID),
		attribute.String("project.slug", projectSlug),
	))
}

// recordPR is called when a PR's lifecycle ends (merged | conflict | abandoned).
func (m *Metrics) recordPR(ctx context.Context, agentID, status string) {
	if m == nil || m.prs == nil {
		return
	}
	m.prs.Add(ctx, 1, metric.WithAttributes(
		attribute.String("agent.id", agentID),
		attribute.String("status", status),
	))
}

// recordCIRun is called per CI invocation by the merger; the merger
// looks up its Metrics handle through the orchestrator.
func (m *Metrics) recordCIRun(ctx context.Context, agentID, result string) {
	if m == nil || m.ciRuns == nil {
		return
	}
	m.ciRuns.Add(ctx, 1, metric.WithAttributes(
		attribute.String("agent.id", agentID),
		attribute.String("result", result),
	))
}

// setQueueDepth is called periodically with the current open-issue count
// per priority label.
func (m *Metrics) setQueueDepth(ctx context.Context, projectSlug, priority string, depth int64) {
	if m == nil || m.queueDepth == nil {
		return
	}
	m.queueDepth.Record(ctx, depth, metric.WithAttributes(
		attribute.String("project.slug", projectSlug),
		attribute.String("priority", priority),
	))
}
