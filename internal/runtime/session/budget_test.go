package session

import (
	"testing"
)

func TestContextBudgetForModel(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int
		wantReserved  int
		wantCompact   int
	}{
		{"small 32K", 32_000, 8_000, 12_000},
		{"medium 64K", 64_000, 16_000, 24_000},
		{"large 128K", 128_000, 30_000, 49_000},
		{"claude 200K", 200_000, 40_000, 80_000},
		{"huge 1M", 1_000_000, 200_000, 400_000},
		{"zero", 0, 40_000, 100_000}, // defaults
		{"negative", -1, 40_000, 100_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := ContextBudgetForModel(tt.contextWindow)
			if b.ReservedTokens != tt.wantReserved {
				t.Errorf("reserved: got %d, want %d", b.ReservedTokens, tt.wantReserved)
			}
			if b.CompactionThreshold != tt.wantCompact {
				t.Errorf("compaction: got %d, want %d", b.CompactionThreshold, tt.wantCompact)
			}
		})
	}
}

func TestContextBudget_EffectiveMax(t *testing.T) {
	b := ContextBudgetForModel(200_000)
	if b.EffectiveMax() != 160_000 {
		t.Errorf("effective max: got %d, want 160000", b.EffectiveMax())
	}
}

func TestContextBudget_Thresholds(t *testing.T) {
	b := ContextBudgetForModel(200_000)

	softAt := b.SoftTrimAt()
	hardAt := b.HardClearAt()

	if softAt >= hardAt {
		t.Errorf("soft (%d) should be < hard (%d)", softAt, hardAt)
	}
	if hardAt >= b.CompactionThreshold {
		t.Errorf("hard (%d) should be < compaction (%d)", hardAt, b.CompactionThreshold)
	}
}

func TestDefaultContextBudget(t *testing.T) {
	b := DefaultContextBudget()
	if b.CompactionThreshold != CompactionThreshold {
		t.Errorf("default should match CompactionThreshold constant")
	}
	if b.MaxChatHistoryTokens <= 0 {
		t.Errorf("MaxChatHistoryTokens should be > 0, got %d", b.MaxChatHistoryTokens)
	}
}

func TestChatHistoryBudget(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int
		expectedMin   int
		expectedMax   int
	}{
		{"32K context", 32_000, 1024, 2048},
		{"128K context", 128_000, 8000, 8192},
		{"200K context", 200_000, 8192, 8192},
		{"1M context", 1_000_000, 8192, 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := ContextBudgetForModel(tt.contextWindow)
			if budget.MaxChatHistoryTokens < tt.expectedMin {
				t.Errorf("MaxChatHistoryTokens %d < expected min %d",
					budget.MaxChatHistoryTokens, tt.expectedMin)
			}
			if budget.MaxChatHistoryTokens > tt.expectedMax {
				t.Errorf("MaxChatHistoryTokens %d > expected max %d",
					budget.MaxChatHistoryTokens, tt.expectedMax)
			}
		})
	}
}

func TestChatHistoryBudget_NonCachingProvider(t *testing.T) {
	budgetCaching := ContextBudgetForProvider(200_000, true)
	budgetNoCaching := ContextBudgetForProvider(200_000, false)

	if budgetNoCaching.MaxChatHistoryTokens >= budgetCaching.MaxChatHistoryTokens {
		t.Errorf("non-caching history budget (%d) should be less than caching (%d)",
			budgetNoCaching.MaxChatHistoryTokens, budgetCaching.MaxChatHistoryTokens)
	}
	if budgetNoCaching.MaxChatHistoryTokens > 4096 {
		t.Errorf("non-caching history budget (%d) should be <= 4096",
			budgetNoCaching.MaxChatHistoryTokens)
	}
}

func TestEnforceSummaryCap(t *testing.T) {
	small := "short summary"
	result := EnforceSummaryCap(small, 1000)
	if result != small {
		t.Error("small summary should be unchanged")
	}

	large := make([]byte, 40000)
	for i := range large {
		large[i] = 'x'
	}
	result = EnforceSummaryCap(string(large), 2000)
	if len(result) >= len(large) {
		t.Errorf("large summary should be truncated: got %d chars", len(result))
	}
}

func TestEnforceSummaryCap_ZeroBudget(t *testing.T) {
	result := EnforceSummaryCap("test", 0)
	if result != "test" {
		t.Error("zero budget should return summary unchanged")
	}
}

func TestShouldCompact_RatioBased(t *testing.T) {
	budget := ContextBudgetForModel(200_000)
	// Below threshold — should not compact.
	if budget.ShouldCompact(50_000) {
		t.Error("50K tokens should not trigger compaction")
	}
	// Above threshold — should compact.
	if !budget.ShouldCompact(budget.CompactionThreshold + 1) {
		t.Error("above CompactionThreshold should trigger compaction")
	}
}

func TestShouldCompact_ReservedBuffer(t *testing.T) {
	budget := ContextBudgetForModel(200_000)
	// Below ratio threshold but close to context limit with reserved buffer.
	nearLimit := budget.ContextWindow - budget.ReservedTokens - budget.ReservedBuffer
	if !budget.ShouldCompact(nearLimit) {
		t.Errorf("at %d tokens (near limit), should trigger via reserved buffer", nearLimit)
	}
}

func TestReservedBuffer_Proportional(t *testing.T) {
	b32 := ContextBudgetForModel(32_000)
	b200 := ContextBudgetForModel(200_000)

	if b32.ReservedBuffer >= b200.ReservedBuffer {
		t.Errorf("32K buffer (%d) should be less than 200K buffer (%d)",
			b32.ReservedBuffer, b200.ReservedBuffer)
	}
	if b200.ReservedBuffer != 20_000 {
		t.Errorf("200K context should have 20K reserved buffer, got %d", b200.ReservedBuffer)
	}
}
