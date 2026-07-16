package otel

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestQuietErrorHandlerLogsCollectorUnavailableOnce(t *testing.T) {
	var buf bytes.Buffer
	restore := captureSlog(t, &buf)
	defer restore()

	otelSDKErrorLogSuppressor = newOncePerClassLogSuppressor()
	err := errors.New("failed to export metrics: rpc error: code = Unavailable desc = connection refused")

	h := quietErrorHandler{}
	h.Handle(err)
	h.Handle(err)
	h.Handle(err)

	logs := buf.String()
	if got := strings.Count(logs, "otel: collector unavailable"); got != 1 {
		t.Fatalf("collector unavailable log count = %d, want 1\nlogs:\n%s", got, logs)
	}
	if strings.Contains(logs, "otel: sdk error") {
		t.Fatalf("collector unavailable should not log as generic sdk error\nlogs:\n%s", logs)
	}
}

func TestQuietErrorHandlerLogsGenericSDKErrorOnce(t *testing.T) {
	var buf bytes.Buffer
	restore := captureSlog(t, &buf)
	defer restore()

	otelSDKErrorLogSuppressor = newOncePerClassLogSuppressor()
	err := errors.New("failed to encode export batch")

	h := quietErrorHandler{}
	h.Handle(err)
	h.Handle(err)

	logs := buf.String()
	if got := strings.Count(logs, "otel: sdk error"); got != 1 {
		t.Fatalf("sdk error log count = %d, want 1\nlogs:\n%s", got, logs)
	}
}

func captureSlog(t *testing.T, buf *bytes.Buffer) func() {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return func() { slog.SetDefault(prev) }
}
