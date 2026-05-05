package memory

import (
	"testing"
	"time"
)

func TestUpdateValueOnAccess(t *testing.T) {
	mem := &Memory{Name: "test"}
	if mem.AccessCount != 0 {
		t.Fatal("initial access count should be 0")
	}

	UpdateValueOnAccess(mem)
	if mem.AccessCount != 1 {
		t.Errorf("access count = %d, want 1", mem.AccessCount)
	}
	if mem.LastAccessedAt.IsZero() {
		t.Error("last accessed at should be set")
	}

	UpdateValueOnAccess(mem)
	if mem.AccessCount != 2 {
		t.Errorf("access count = %d, want 2", mem.AccessCount)
	}
}

func TestPropagateReward_Basic(t *testing.T) {
	mem := &Memory{Name: "test", Importance: 0.5}

	// High reward should increase value.
	PropagateReward(mem, 1.0, 0.3)
	if mem.ValueScore <= 0.5 {
		t.Errorf("value should increase with high reward, got %f", mem.ValueScore)
	}

	// Expected: 0.3*1.0 + 0.7*0.5 = 0.65
	expected := 0.65
	if diff := mem.ValueScore - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("value = %f, want ~%f", mem.ValueScore, expected)
	}
}

func TestPropagateReward_LowReward(t *testing.T) {
	mem := &Memory{Name: "test", ValueScore: 0.8}

	// Low reward should decrease value.
	PropagateReward(mem, 0.1, 0.3)
	if mem.ValueScore >= 0.8 {
		t.Errorf("value should decrease with low reward, got %f", mem.ValueScore)
	}
}

func TestPropagateReward_ClampedInput(t *testing.T) {
	mem := &Memory{Name: "test", ValueScore: 0.5}

	// Negative reward clamped to 0.
	PropagateReward(mem, -1.0, 0.3)
	if mem.ValueScore > 0.5 {
		t.Errorf("negative reward should not increase value, got %f", mem.ValueScore)
	}

	// Reward > 1 clamped to 1.
	mem.ValueScore = 0.5
	PropagateReward(mem, 2.0, 0.3)
	expected := 0.3*1.0 + 0.7*0.5 // = 0.65
	if diff := mem.ValueScore - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("clamped reward: value = %f, want ~%f", mem.ValueScore, expected)
	}
}

func TestPropagateReward_InvalidAlpha(t *testing.T) {
	mem := &Memory{Name: "test", ValueScore: 0.5}

	// alpha=0 should default to DefaultRewardAlpha (0.3).
	PropagateReward(mem, 1.0, 0)
	expected := DefaultRewardAlpha*1.0 + (1-DefaultRewardAlpha)*0.5
	if diff := mem.ValueScore - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("default alpha: value = %f, want ~%f", mem.ValueScore, expected)
	}
}

func TestDecayValue_GracePeriod(t *testing.T) {
	mem := &Memory{
		Name:           "test",
		ValueScore:     0.8,
		LastAccessedAt: time.Now().Add(-3 * 24 * time.Hour), // 3 days ago
	}

	DecayValue(mem, 30)
	if mem.ValueScore != 0.8 {
		t.Errorf("within grace period: value should not decay, got %f", mem.ValueScore)
	}
}

func TestDecayValue_AfterGrace(t *testing.T) {
	mem := &Memory{
		Name:           "test",
		ValueScore:     0.8,
		LastAccessedAt: time.Now().Add(-30 * 24 * time.Hour), // 30 days ago
	}

	DecayValue(mem, 30)
	if mem.ValueScore >= 0.8 {
		t.Errorf("after grace: value should decay, got %f", mem.ValueScore)
	}
	if mem.ValueScore <= 0 {
		t.Errorf("value should not decay to zero, got %f", mem.ValueScore)
	}
}

func TestDecayValue_UsesUpdateTimeAsFallback(t *testing.T) {
	mem := &Memory{
		Name:       "test",
		ValueScore: 0.8,
		UpdatedAt:  time.Now().Add(-60 * 24 * time.Hour), // 60 days ago, no access time
	}

	DecayValue(mem, 30)
	if mem.ValueScore >= 0.8 {
		t.Errorf("should decay using UpdatedAt as fallback, got %f", mem.ValueScore)
	}
}

func TestDecayValue_ZeroHalfLife(t *testing.T) {
	mem := &Memory{
		Name:           "test",
		ValueScore:     0.8,
		LastAccessedAt: time.Now().Add(-30 * 24 * time.Hour),
	}

	// halfLifeDays=0 should default to 30.
	DecayValue(mem, 0)
	if mem.ValueScore >= 0.8 {
		t.Errorf("should still decay with default half-life, got %f", mem.ValueScore)
	}
}

func TestEffectiveValue(t *testing.T) {
	tests := []struct {
		name      string
		mem       Memory
		wantValue float64
	}{
		{"value score set", Memory{ValueScore: 0.8}, 0.8},
		{"importance only", Memory{Importance: 0.7}, 0.7},
		{"both set, value wins", Memory{ValueScore: 0.9, Importance: 0.3}, 0.9},
		{"neither set", Memory{}, 0.5},
		{"zero value, importance set", Memory{ValueScore: 0, Importance: 0.6}, 0.6},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.mem.EffectiveValue()
			if got != tc.wantValue {
				t.Errorf("EffectiveValue = %f, want %f", got, tc.wantValue)
			}
		})
	}
}
