//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPerformance(t *testing.T) {
	requireConnectivity(t)

	t.Run("HealthEndpointLatency", func(t *testing.T) {
		const numRequests = 50
		url := baseURL(t) + "/healthz"
		latencies := make([]time.Duration, 0, numRequests)

		for i := 0; i < numRequests; i++ {
			start := time.Now()
			resp, err := httpClient().Get(url)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("request %d failed: %v", i, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("request %d returned %d", i, resp.StatusCode)
			}
			latencies = append(latencies, elapsed)
		}

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

		p50 := latencies[len(latencies)*50/100]
		p95 := latencies[len(latencies)*95/100]
		p99 := latencies[len(latencies)*99/100]

		var total time.Duration
		for _, d := range latencies {
			total += d
		}
		mean := total / time.Duration(len(latencies))

		t.Logf("Health endpoint latency (%d requests):", numRequests)
		t.Logf("  mean: %s", mean)
		t.Logf("  p50:  %s", p50)
		t.Logf("  p95:  %s", p95)
		t.Logf("  p99:  %s", p99)

		// Soft threshold: warn but don't fail for p99 > 1s.
		if p99 > 1*time.Second {
			t.Logf("WARNING: p99 latency %s exceeds 1s", p99)
		}
	})

	t.Run("TraceIngestionThroughput", func(t *testing.T) {
		const numBatches = 100
		url := baseURL(t) + "/collector/v1/traces"
		payload := `{"resourceSpans":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"perf-test"}}]},"scopeSpans":[{"spans":[{"traceId":"00000000000000000000000000000002","spanId":"0000000000000099","name":"perf-span","kind":1,"startTimeUnixNano":"1000000000","endTimeUnixNano":"2000000000"}]}]}]}`

		start := time.Now()
		var wg sync.WaitGroup
		errors := make(chan error, numBatches)

		for i := 0; i < numBatches; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := httpClient().Post(url, "application/json", strings.NewReader(payload))
				if err != nil {
					errors <- err
					return
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("status %d", resp.StatusCode)
				}
			}()
		}

		wg.Wait()
		close(errors)
		elapsed := time.Since(start)

		var errs []error
		for err := range errors {
			errs = append(errs, err)
		}

		rps := float64(numBatches) / elapsed.Seconds()
		t.Logf("Trace ingestion: %d batches in %s (%.0f req/s)", numBatches, elapsed, rps)

		if len(errs) > 0 {
			t.Errorf("%d/%d requests failed; first error: %v", len(errs), numBatches, errs[0])
		}
	})

	t.Run("BinaryStartupTime", func(t *testing.T) {
		bin := binaryPath()
		if bin == "" {
			t.Skip("ycode binary not found")
		}
		if !isLocal(t) {
			t.Skip("binary startup test only runs locally")
		}

		// Warm up.
		exec.Command(bin, "version").Run()

		const runs = 5
		latencies := make([]time.Duration, 0, runs)
		for i := 0; i < runs; i++ {
			start := time.Now()
			out, err := exec.Command(bin, "version").CombinedOutput()
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("ycode version failed: %v\n%s", err, out)
			}
			latencies = append(latencies, elapsed)
		}

		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		median := latencies[len(latencies)/2]
		t.Logf("Binary startup time (median of %d runs): %s", runs, median)

		if median > 2*time.Second {
			t.Logf("WARNING: median startup time %s exceeds 2s", median)
		}
	})
}
