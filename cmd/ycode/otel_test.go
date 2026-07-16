package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/config"
)

func TestResolveCollectorAddrNoImplicitLoopbackDefault(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("HOME", t.TempDir())
	prevOverride := overrideCollectorAddr
	overrideCollectorAddr = ""
	t.Cleanup(func() { overrideCollectorAddr = prevOverride })

	cfg := &config.Config{Observability: &config.ObservabilityConfig{}}
	if got := resolveCollectorAddr(cfg); got != "" {
		t.Fatalf("resolveCollectorAddr without env/config/discovery = %q, want empty", got)
	}
}

func TestResolveCollectorAddrForEnabledObservabilityHonorsOptOut(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317")
	prevOverride := overrideCollectorAddr
	overrideCollectorAddr = ""
	t.Cleanup(func() { overrideCollectorAddr = prevOverride })

	enabled := false
	cfg := &config.Config{Observability: &config.ObservabilityConfig{Enabled: &enabled}}
	if got := resolveCollectorAddrForEnabledObservability(cfg); got != "" {
		t.Fatalf("disabled observability collector addr = %q, want empty", got)
	}
}

func TestResolveCollectorAddrUsesConfiguredEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("HOME", t.TempDir())
	prevOverride := overrideCollectorAddr
	overrideCollectorAddr = ""
	t.Cleanup(func() { overrideCollectorAddr = prevOverride })

	cfg := &config.Config{Observability: &config.ObservabilityConfig{CollectorAddr: "collector.example:4317"}}
	if got := resolveCollectorAddrForEnabledObservability(cfg); got != "collector.example:4317" {
		t.Fatalf("configured collector addr = %q, want collector.example:4317", got)
	}
}

func TestResolveCollectorAddrUsesDiscoveryFile(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	prevOverride := overrideCollectorAddr
	overrideCollectorAddr = ""
	t.Cleanup(func() { overrideCollectorAddr = prevOverride })

	dir := filepath.Join(home, ".agents", "ycode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "collector.addr"), []byte("127.0.0.1:9999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Observability: &config.ObservabilityConfig{}}
	if got := resolveCollectorAddrForEnabledObservability(cfg); got != "127.0.0.1:9999" {
		t.Fatalf("discovered collector addr = %q, want 127.0.0.1:9999", got)
	}
}
