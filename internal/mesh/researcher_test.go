package mesh

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

func TestResearcher_RateLimiting(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	// Limit to 2 searches per window.
	researcher := NewResearcher(mb, 2)

	var searchCount atomic.Int32
	researcher.SearchFunc = func(_ context.Context, query string) (string, error) {
		searchCount.Add(1)
		return "result for: " + query, nil
	}
	researcher.SaveFunc = func(_ context.Context, _, _ string) error {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := researcher.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer researcher.Stop()
	time.Sleep(50 * time.Millisecond) // let goroutine subscribe

	resultCh, unsub := mb.Subscribe(bus.EventResearchDone)
	defer unsub()

	// Send 4 events; only 2 should trigger searches.
	for i := 0; i < 4; i++ {
		report := map[string]string{
			"id":       "report-" + string(rune('a'+i)),
			"severity": "warn",
			"summary":  "error " + string(rune('a'+i)),
		}
		data, _ := json.Marshal(report)
		mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: data})
	}

	// Wait for processing.
	received := 0
	timeout := time.After(2 * time.Second)
	for received < 2 {
		select {
		case <-resultCh:
			received++
		case <-timeout:
			break
		}
	}

	// Give a little extra time for any spurious processing.
	time.Sleep(100 * time.Millisecond)

	got := int(searchCount.Load())
	if got != 2 {
		t.Fatalf("expected 2 searches (rate limited), got %d", got)
	}
}

func TestResearcher_QueryTruncation(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	researcher := NewResearcher(mb, 10)

	var capturedQuery string
	researcher.SearchFunc = func(_ context.Context, query string) (string, error) {
		capturedQuery = query
		return "result", nil
	}
	researcher.SaveFunc = func(_ context.Context, _, _ string) error {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := researcher.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer researcher.Stop()
	time.Sleep(50 * time.Millisecond) // let goroutine subscribe

	resultCh, unsub := mb.Subscribe(bus.EventResearchDone)
	defer unsub()

	// Give the goroutine time to subscribe before publishing.
	time.Sleep(50 * time.Millisecond)

	// Send a long query.
	longData := make([]byte, 300)
	for i := range longData {
		longData[i] = 'x'
	}
	mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: longData})

	select {
	case <-resultCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for research result")
	}

	if len(capturedQuery) != 200 {
		t.Fatalf("expected query truncated to 200, got %d", len(capturedQuery))
	}
}

func TestResearcher_NilFuncsSkipped(t *testing.T) {
	mb := bus.NewMemoryBus()
	defer mb.Close()

	researcher := NewResearcher(mb, 10)
	// Leave SearchFunc and SaveFunc nil.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := researcher.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer researcher.Stop()
	time.Sleep(50 * time.Millisecond) // let goroutine subscribe

	report := map[string]string{"id": "r1", "severity": "warn", "summary": "test"}
	data, _ := json.Marshal(report)
	mb.Publish(bus.Event{Type: bus.EventDiagReport, Data: data})

	// Give time for processing; no panic should occur.
	time.Sleep(100 * time.Millisecond)

	// If we get here without panic, test passes.
}
