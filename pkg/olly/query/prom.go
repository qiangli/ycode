package query

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/qiangli/ycode/pkg/otel/prometheus/tsdb"
)

// PromAdapter implements MetricsBackend over an in-process Prometheus
// TSDB and PromQL engine. Both must be already constructed (typically by
// internal/observability/PrometheusComponent) and shared with this adapter.
//
// Calls bypass the Prometheus HTTP API entirely — same engine, same
// storage, no serialization round-trip.
type PromAdapter struct {
	DB     *tsdb.DB
	Engine *promql.Engine
}

// NewPromAdapter returns a PromAdapter; both arguments must be non-nil.
func NewPromAdapter(db *tsdb.DB, engine *promql.Engine) *PromAdapter {
	return &PromAdapter{DB: db, Engine: engine}
}

// Instant evaluates an instant PromQL query.
func (p *PromAdapter) Instant(ctx context.Context, expr string, at time.Time) (Result, error) {
	if p == nil || p.DB == nil || p.Engine == nil {
		return Result{}, ErrUnsupported
	}
	q, err := p.Engine.NewInstantQuery(ctx, p.DB, nil, expr, at)
	if err != nil {
		return Result{}, fmt.Errorf("instant query: %w", err)
	}
	defer q.Close()
	res := q.Exec(ctx)
	if res.Err != nil {
		return Result{}, fmt.Errorf("eval: %w", res.Err)
	}
	return convertPromResult(res), nil
}

// Range evaluates a range PromQL query.
func (p *PromAdapter) Range(ctx context.Context, expr string, start, end time.Time, step time.Duration) (Result, error) {
	if p == nil || p.DB == nil || p.Engine == nil {
		return Result{}, ErrUnsupported
	}
	q, err := p.Engine.NewRangeQuery(ctx, p.DB, nil, expr, start, end, step)
	if err != nil {
		return Result{}, fmt.Errorf("range query: %w", err)
	}
	defer q.Close()
	res := q.Exec(ctx)
	if res.Err != nil {
		return Result{}, fmt.Errorf("eval: %w", res.Err)
	}
	return convertPromResult(res), nil
}

// convertPromResult turns a promql.Result into the package-neutral Result
// shape so callers don't have to depend on Prometheus types directly.
func convertPromResult(res *promql.Result) Result {
	out := Result{Type: string(res.Value.Type())}
	switch v := res.Value.(type) {
	case promql.Vector:
		out.Series = make([]Series, 0, len(v))
		for _, s := range v {
			out.Series = append(out.Series, Series{
				Labels:  labelsToMap(s.Metric),
				Samples: []Sample{{Time: time.UnixMilli(s.T), Value: s.F}},
			})
		}
	case promql.Matrix:
		out.Series = make([]Series, 0, len(v))
		for _, m := range v {
			samples := make([]Sample, 0, len(m.Floats))
			for _, fp := range m.Floats {
				samples = append(samples, Sample{Time: time.UnixMilli(fp.T), Value: fp.F})
			}
			out.Series = append(out.Series, Series{
				Labels:  labelsToMap(m.Metric),
				Samples: samples,
			})
		}
	case promql.Scalar:
		out.Series = []Series{{
			Samples: []Sample{{Time: time.UnixMilli(v.T), Value: v.V}},
		}}
	}
	return out
}

// labelsToMap flattens Prometheus labels into a string map.
func labelsToMap(ls labels.Labels) map[string]string {
	m := make(map[string]string, ls.Len())
	ls.Range(func(l labels.Label) {
		m[l.Name] = l.Value
	})
	return m
}
