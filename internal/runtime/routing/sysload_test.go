package routing

import (
	"testing"
	"time"
)

func TestSysLoadProvider_CachesValue(t *testing.T) {
	sp := NewSysLoadProvider(1 * time.Second)

	// First call samples the system.
	load1 := sp.LoadAverage()

	// Second call within interval should return cached value.
	load2 := sp.LoadAverage()

	// Both should be equal (cached).
	if load1 != load2 {
		t.Errorf("cached values should be equal, got %f and %f", load1, load2)
	}
}

func TestSysLoadProvider_DefaultInterval(t *testing.T) {
	sp := NewSysLoadProvider(0)
	if sp.interval != 5*time.Second {
		t.Errorf("default interval should be 5s, got %v", sp.interval)
	}
}

func TestSysLoadProvider_NoPanic(t *testing.T) {
	sp := NewSysLoadProvider(100 * time.Millisecond)
	// Just verify it doesn't panic on any platform.
	load := sp.LoadAverage()
	if load < 0 {
		t.Errorf("load average should not be negative, got %f", load)
	}
}

func TestReadLoadAverage_NoPanic(t *testing.T) {
	// Platform-independent smoke test — should not panic.
	load := readLoadAverage()
	_ = load // value depends on platform
}
