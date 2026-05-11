package otel

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// End-to-end coverage for exec_metrics.go: install an in-memory
// MeterProvider, fire StartExecSpan / RecordExec / RecordShellBuiltin
// for several scopes, then assert that the expected counters +
// histograms appear with the right attribute sets. Complements the
// per-function unit tests in exec_metrics_test.go (which exercise
// classification and lifecycle but don't check metric emission).

func withMeter(t *testing.T, fn func()) metricdata.ResourceMetrics {
	t.Helper()
	prev := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	fn()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

// counterValue scans rm for an Int64 counter with the given name and an
// attribute subset, returning the summed value across matching data
// points. Returns 0 if no point matches. Handles both string and bool
// attribute types — bool values must be passed in the map as "true"
// or "false".
func counterValue(rm metricdata.ResourceMetrics, name string, attrs map[string]string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				match := true
				for k, v := range attrs {
					got, ok := dp.Attributes.Value(attribute.Key(k))
					if !ok {
						match = false
						break
					}
					var asStr string
					switch got.Type() {
					case attribute.BOOL:
						if got.AsBool() {
							asStr = "true"
						} else {
							asStr = "false"
						}
					default:
						asStr = got.AsString()
					}
					if asStr != v {
						match = false
						break
					}
				}
				if match {
					total += dp.Value
				}
			}
		}
	}
	return total
}

// hasHistogram reports whether a Float64 histogram named `name` exists
// with at least one data point.
func hasHistogram(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok {
				return len(h.DataPoints) > 0
			}
		}
	}
	return false
}

func TestE2E_StartExecSpan_EmitsCounterAndHistogram(t *testing.T) {
	rm := withMeter(t, func() {
		ctx, finish := StartExecSpan(context.Background(), ExecScopeBash, "/bin/echo", []string{"/bin/echo", "hi"})
		_ = ctx
		finish(0, nil)
	})

	got := counterValue(rm, "ycode.exec.total", map[string]string{
		"scope":      ExecScopeBash,
		"exit_class": ExitClassZero,
	})
	if got != 1 {
		t.Fatalf("ycode.exec.total{scope=bash,exit_class=zero} = %d; want 1", got)
	}
	if !hasHistogram(rm, "ycode.exec.duration") {
		t.Fatal("ycode.exec.duration histogram missing")
	}
}

func TestE2E_MultipleScopes(t *testing.T) {
	// Same call site invoked under three different scopes should
	// produce three separate timeseries.
	rm := withMeter(t, func() {
		_, f1 := StartExecSpan(context.Background(), ExecScopeBash, "/bin/echo", nil)
		f1(0, nil)
		_, f2 := StartExecSpan(context.Background(), ExecScopeToolexec, "/usr/bin/git", nil)
		f2(0, nil)
		_, f3 := StartExecSpan(context.Background(), ExecScopeSandbox, "/usr/bin/podman", nil)
		f3(0, nil)
	})

	for _, scope := range []string{ExecScopeBash, ExecScopeToolexec, ExecScopeSandbox} {
		got := counterValue(rm, "ycode.exec.total", map[string]string{
			"scope":      scope,
			"exit_class": ExitClassZero,
		})
		if got != 1 {
			t.Errorf("ycode.exec.total{scope=%s} = %d; want 1", scope, got)
		}
	}
}

func TestE2E_FailureClassification(t *testing.T) {
	rm := withMeter(t, func() {
		// real failing process → ExitError with exit code 2
		cmd := exec.Command("/bin/sh", "-c", "exit 2")
		runErr := cmd.Run()
		_, finish := StartExecSpan(context.Background(), ExecScopeBash, "/bin/sh", nil)
		finish(cmd.ProcessState.ExitCode(), runErr)

		// synthetic "not found"
		_, finish2 := StartExecSpan(context.Background(), ExecScopeToolexec, "/nope", nil)
		finish2(0, &exec.Error{Name: "/nope", Err: errors.New("missing")})
	})

	if got := counterValue(rm, "ycode.exec.total", map[string]string{
		"scope":      ExecScopeBash,
		"exit_class": ExitClassError,
	}); got != 1 {
		t.Errorf("error-class counter for bash = %d; want 1", got)
	}
	if got := counterValue(rm, "ycode.exec.total", map[string]string{
		"scope":      ExecScopeToolexec,
		"exit_class": ExitClassNotFound,
	}); got != 1 {
		t.Errorf("not-found-class counter for toolexec = %d; want 1", got)
	}
}

func TestE2E_RecordExec_PostHoc(t *testing.T) {
	rm := withMeter(t, func() {
		RecordExec(context.Background(), ExecScopeContainer, "/usr/bin/podman", 10*time.Millisecond, 0, nil)
		RecordExec(context.Background(), ExecScopeContainer, "/usr/bin/podman", 5*time.Millisecond, 1, errors.New("boom"))
	})

	if got := counterValue(rm, "ycode.exec.total", map[string]string{
		"scope":      ExecScopeContainer,
		"exit_class": ExitClassZero,
	}); got != 1 {
		t.Errorf("post-hoc success counter = %d; want 1", got)
	}
	if got := counterValue(rm, "ycode.exec.total", map[string]string{
		"scope":      ExecScopeContainer,
		"exit_class": ExitClassError,
	}); got != 1 {
		t.Errorf("post-hoc error counter = %d; want 1", got)
	}
}

func TestE2E_RecordShellBuiltin(t *testing.T) {
	rm := withMeter(t, func() {
		RecordShellBuiltin(context.Background(), "sandbox", 12*time.Millisecond, 0)
		RecordShellBuiltin(context.Background(), "sandbox", 8*time.Millisecond, 0)
		RecordShellBuiltin(context.Background(), "git", 3*time.Millisecond, 1)
	})

	if got := counterValue(rm, "ycode.shell.builtin.total", map[string]string{
		"verb": "sandbox",
	}); got != 2 {
		t.Errorf("yc sandbox counter = %d; want 2", got)
	}
	if got := counterValue(rm, "ycode.shell.builtin.total", map[string]string{
		"verb":    "git",
		"success": "false",
	}); got != 1 {
		t.Errorf("yc git failure counter = %d; want 1", got)
	}
	if !hasHistogram(rm, "ycode.shell.builtin.duration") {
		t.Fatal("ycode.shell.builtin.duration histogram missing")
	}
}

// Sanity: counterValue returns "false" as the string form of a Bool
// attribute on success=false; verify that explicitly so the assertion
// in TestE2E_RecordShellBuiltin can't silently false-match.
func TestE2E_BoolAttrEncoding(t *testing.T) {
	rm := withMeter(t, func() {
		RecordShellBuiltin(context.Background(), "test-verb", 1*time.Millisecond, 0)
	})
	if got := counterValue(rm, "ycode.shell.builtin.total", map[string]string{
		"verb":    "test-verb",
		"success": "true",
	}); got != 1 {
		t.Fatalf("success=true attr should encode as \"true\"; got count %d", got)
	}
}
