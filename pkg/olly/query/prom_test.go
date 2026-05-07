//go:build integration

package query

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/qiangli/ycode/pkg/otel/prometheus/tsdb"
)

// TestPromAdapter_RoundTrip writes a sample to a fresh TSDB and reads it
// back through the typed query API. Tagged integration because TSDB
// initialization touches the filesystem and is non-trivial.
func TestPromAdapter_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := tsdb.Open(dir, nil, nil, &tsdb.Options{
		RetentionDuration: int64(24 * time.Hour / time.Millisecond),
		MinBlockDuration:  int64(2 * time.Hour / time.Millisecond),
		MaxBlockDuration:  int64(36 * time.Hour / time.Millisecond),
	}, nil)
	if err != nil {
		t.Fatalf("open tsdb: %v", err)
	}
	defer db.Close()

	app := db.Appender(context.Background())
	now := time.Now().UnixMilli()
	lset := labels.FromStrings("__name__", "ycode_turn_total", "service_name", "ycode")
	if _, err := app.Append(0, lset, now, 7); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := app.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	engine := promql.NewEngine(promql.EngineOpts{
		MaxSamples:    1_000_000,
		Timeout:       10 * time.Second,
		LookbackDelta: 5 * time.Minute,
	})
	adapter := NewPromAdapter(db, engine)

	res, err := adapter.Instant(context.Background(), `ycode_turn_total{service_name="ycode"}`, time.UnixMilli(now))
	if err != nil {
		t.Fatalf("instant query: %v", err)
	}
	if len(res.Series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(res.Series))
	}
	if res.Series[0].Labels["service_name"] != "ycode" {
		t.Errorf("service label lost: %v", res.Series[0].Labels)
	}
	if len(res.Series[0].Samples) != 1 || res.Series[0].Samples[0].Value != 7 {
		t.Errorf("unexpected samples: %+v", res.Series[0].Samples)
	}
}
