package routing

import (
	"context"
	"testing"
)

func TestSQLStatsProvider_NilStore(t *testing.T) {
	sp := &SQLStatsProvider{Store: nil}
	stats := sp.Stats(context.Background(), "model", TaskClassification)
	if stats.SampleCount != 0 {
		t.Error("nil store should return empty stats")
	}
}

// Integration test with real SQLite would require storage setup.
// For unit tests, we verify the nil/empty path.
func TestSQLStatsProvider_DefaultLookBack(t *testing.T) {
	sp := NewSQLStatsProvider(nil)
	if sp.LookBack <= 0 {
		t.Error("default LookBack should be positive")
	}
	if sp.MaxSample <= 0 {
		t.Error("default MaxSample should be positive")
	}
}
