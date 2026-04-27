package loop

import "testing"

func TestDifficultyLevel_String(t *testing.T) {
	tests := []struct {
		level DifficultyLevel
		want  string
	}{
		{DifficultyEasy, "easy"},
		{DifficultyMedium, "medium"},
		{DifficultyHard, "hard"},
		{DifficultyExtreme, "extreme"},
		{DifficultyLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("DifficultyLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestDefaultCurriculum(t *testing.T) {
	cfg := DefaultCurriculum()
	if cfg.PromotionThreshold != 0.7 {
		t.Errorf("PromotionThreshold = %f, want 0.7", cfg.PromotionThreshold)
	}
	if cfg.DemotionThreshold != 0.3 {
		t.Errorf("DemotionThreshold = %f, want 0.3", cfg.DemotionThreshold)
	}
	if cfg.MinSamples != 20 {
		t.Errorf("MinSamples = %d, want 20", cfg.MinSamples)
	}
}

func TestCurriculumState_RecordResult(t *testing.T) {
	s := &CurriculumState{}
	s.RecordResult(true)
	s.RecordResult(false)
	s.RecordResult(true)

	if s.Total != 3 {
		t.Errorf("Total = %d, want 3", s.Total)
	}
	if s.PassCount != 2 {
		t.Errorf("PassCount = %d, want 2", s.PassCount)
	}
}

func TestCurriculumState_PassRate(t *testing.T) {
	s := &CurriculumState{}
	if got := s.PassRate(); got != 0 {
		t.Errorf("PassRate() on empty = %f, want 0", got)
	}

	s.RecordResult(true)
	s.RecordResult(true)
	s.RecordResult(false)
	if got := s.PassRate(); got < 0.666 || got > 0.667 {
		t.Errorf("PassRate() = %f, want ~0.667", got)
	}
}

func TestCurriculumState_ShouldPromote(t *testing.T) {
	cfg := &CurriculumConfig{
		PromotionThreshold: 0.7,
		MinSamples:         3,
	}

	// Not enough samples.
	s := &CurriculumState{Level: DifficultyEasy, PassCount: 2, Total: 2}
	if s.ShouldPromote(cfg) {
		t.Error("should not promote with insufficient samples")
	}

	// Enough samples, high pass rate.
	s = &CurriculumState{Level: DifficultyEasy, PassCount: 3, Total: 3}
	if !s.ShouldPromote(cfg) {
		t.Error("should promote with 100% pass rate and enough samples")
	}

	// At extreme level, cannot promote.
	s = &CurriculumState{Level: DifficultyExtreme, PassCount: 3, Total: 3}
	if s.ShouldPromote(cfg) {
		t.Error("should not promote at extreme level")
	}

	// Low pass rate.
	s = &CurriculumState{Level: DifficultyEasy, PassCount: 1, Total: 3}
	if s.ShouldPromote(cfg) {
		t.Error("should not promote with low pass rate")
	}
}

func TestCurriculumState_ShouldDemote(t *testing.T) {
	cfg := &CurriculumConfig{
		DemotionThreshold: 0.3,
		MinSamples:        3,
	}

	// Not enough samples.
	s := &CurriculumState{Level: DifficultyMedium, PassCount: 0, Total: 2}
	if s.ShouldDemote(cfg) {
		t.Error("should not demote with insufficient samples")
	}

	// Enough samples, low pass rate.
	s = &CurriculumState{Level: DifficultyMedium, PassCount: 0, Total: 3}
	if !s.ShouldDemote(cfg) {
		t.Error("should demote with 0% pass rate")
	}

	// At easy level, cannot demote.
	s = &CurriculumState{Level: DifficultyEasy, PassCount: 0, Total: 3}
	if s.ShouldDemote(cfg) {
		t.Error("should not demote at easy level")
	}

	// Pass rate above threshold.
	s = &CurriculumState{Level: DifficultyMedium, PassCount: 2, Total: 3}
	if s.ShouldDemote(cfg) {
		t.Error("should not demote with pass rate above threshold")
	}
}

func TestCurriculumState_Promote(t *testing.T) {
	s := &CurriculumState{Level: DifficultyEasy, PassCount: 10, Total: 15}
	s.Promote()
	if s.Level != DifficultyMedium {
		t.Errorf("Level = %v, want Medium", s.Level)
	}
	if s.PassCount != 0 || s.Total != 0 {
		t.Error("counters should be reset after promotion")
	}

	// Promote to extreme.
	s.Level = DifficultyHard
	s.Promote()
	if s.Level != DifficultyExtreme {
		t.Errorf("Level = %v, want Extreme", s.Level)
	}

	// Cannot promote past extreme.
	s.PassCount = 5
	s.Total = 5
	s.Promote()
	if s.Level != DifficultyExtreme {
		t.Errorf("Level = %v, want Extreme (no change)", s.Level)
	}
	// Counters should NOT be reset since promotion didn't happen.
	if s.PassCount != 5 || s.Total != 5 {
		t.Error("counters should not be reset when promotion is blocked")
	}
}

func TestCurriculumState_Demote(t *testing.T) {
	s := &CurriculumState{Level: DifficultyMedium, PassCount: 3, Total: 20}
	s.Demote()
	if s.Level != DifficultyEasy {
		t.Errorf("Level = %v, want Easy", s.Level)
	}
	if s.PassCount != 0 || s.Total != 0 {
		t.Error("counters should be reset after demotion")
	}

	// Cannot demote past easy.
	s.PassCount = 5
	s.Total = 5
	s.Demote()
	if s.Level != DifficultyEasy {
		t.Errorf("Level = %v, want Easy (no change)", s.Level)
	}
	if s.PassCount != 5 || s.Total != 5 {
		t.Error("counters should not be reset when demotion is blocked")
	}
}
