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
}
