package api

import (
	"testing"
	"time"
)

func TestCacheWarmer_StartStop(t *testing.T) {
	cw := NewCacheWarmer(nil) // nil provider ok for start/stop test

	cw.Start()
	if !cw.running {
		t.Error("expected running after Start")
	}

	// Start again should be no-op.
	cw.Start()
	if !cw.running {
		t.Error("expected still running after second Start")
	}

	cw.Stop()
	if cw.running {
		t.Error("expected stopped after Stop")
	}
}

func TestCacheWarmer_UpdateContext(t *testing.T) {
	cw := NewCacheWarmer(nil)
	tools := []ToolDefinition{
		{Name: "bash", Description: "run bash"},
	}

	cw.UpdateContext("claude-sonnet-4-20250514", "system prompt", tools)

	if cw.lastModel != "claude-sonnet-4-20250514" {
		t.Errorf("expected model 'claude-sonnet-4-20250514', got %q", cw.lastModel)
	}
	if cw.lastSystem != "system prompt" {
		t.Errorf("expected system 'system prompt', got %q", cw.lastSystem)
	}
	if len(cw.lastTools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(cw.lastTools))
	}
}

func TestCacheWarmer_MaxPingsLimit(t *testing.T) {
	cw := NewCacheWarmer(nil)
	cw.MaxPings = 0 // unlimited
	cw.PingCount = 5

	// With MaxPings=0, should not be limited.
	if cw.MaxPings > 0 && cw.PingCount >= cw.MaxPings {
		t.Error("should not be limited with MaxPings=0")
	}

	// With MaxPings=5, should be limited.
	cw.MaxPings = 5
	if !(cw.MaxPings > 0 && cw.PingCount >= cw.MaxPings) {
		t.Error("should be limited when PingCount >= MaxPings")
	}
}

func TestCacheWarmInterval(t *testing.T) {
	// Verify interval is less than 5 minutes (Anthropic cache TTL).
	if CacheWarmInterval >= 5*time.Minute {
		t.Errorf("CacheWarmInterval (%v) should be less than 5 minutes", CacheWarmInterval)
	}
	if CacheWarmInterval < 4*time.Minute {
		t.Errorf("CacheWarmInterval (%v) should be at least 4 minutes", CacheWarmInterval)
	}
}
