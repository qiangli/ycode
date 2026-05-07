package otel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/log"
)

// TestProvider_FileOnlyLogsExporter verifies the file-mode LoggerProvider
// gap fix: with PersistLogs=true and no collector, structured logs land
// on disk. Before this fix, file-mode mode silently dropped all OTel
// logs (only traces and metrics were persisted).
func TestProvider_FileOnlyLogsExporter(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := NewProvider(ctx, ProviderConfig{
		ServiceName:    "ycode-test",
		ServiceVersion: "0.0.0",
		SessionID:      "sess-test",
		InstanceID:     "instance-test",
		SampleRate:     1.0,
		DataDir:        dir,
		InstanceDir:    dir,
		PersistLogs:    true, // no CollectorAddr — file-only path
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.LoggerProvider == nil {
		t.Fatal("LoggerProvider must be created when PersistLogs=true even without a collector")
	}

	// Emit a log via the OTel logs API.
	logger := p.LoggerProvider.Logger("phase-2-3-test")
	var rec log.Record
	rec.SetTimestamp(time.Now())
	rec.SetBody(log.StringValue("hello-from-file-only-mode"))
	rec.SetSeverity(log.SeverityInfo)
	logger.Emit(ctx, rec)

	// Force flush.
	if err := p.LoggerProvider.ForceFlush(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	_ = p.Shutdown(ctx)

	// Verify a log file was written under {dir}/logs/.
	logsDir := filepath.Join(dir, "logs")
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("read logs dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no log file written in file-only mode")
	}
	var content []byte
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(logsDir, e.Name()))
		if err != nil {
			t.Fatalf("read log file: %v", err)
		}
		content = append(content, b...)
	}
	body := string(content)
	if !strings.Contains(body, "hello-from-file-only-mode") {
		t.Errorf("log body missing test message; got:\n%s", body)
	}
	if !strings.Contains(body, "ycode-test") {
		t.Errorf("log body missing service.name resource attribute; got:\n%s", body)
	}
}
