package loop

// DifficultyLevel represents task difficulty.
type DifficultyLevel int

const (
	DifficultyEasy DifficultyLevel = iota
	DifficultyMedium
	DifficultyHard
	DifficultyExtreme
)

func (d DifficultyLevel) String() string {
	switch d {
	case DifficultyEasy:
		return "easy"
	case DifficultyMedium:
		return "medium"
	case DifficultyHard:
		return "hard"
	case DifficultyExtreme:
		return "extreme"
	default:
		return "unknown"
	}
}

// CurriculumConfig configures curriculum learning.
type CurriculumConfig struct {
	// PromotionThreshold: move to next difficulty when pass rate exceeds this.
	PromotionThreshold float64
	// DemotionThreshold: drop back when pass rate falls below this.
	DemotionThreshold float64
	// MinSamples: minimum evaluations before considering promotion.
	MinSamples int
}

// DefaultCurriculum returns sensible defaults.
func DefaultCurriculum() *CurriculumConfig {
	return &CurriculumConfig{
		PromotionThreshold: 0.7,
		DemotionThreshold:  0.3,
		MinSamples:         20,
	}
}

// CurriculumState tracks current difficulty and performance.
type CurriculumState struct {
	Level     DifficultyLevel
	PassCount int
	Total     int
}

// RecordResult updates the curriculum state with a new evaluation result.
func (s *CurriculumState) RecordResult(passed bool) {
	s.Total++
	if passed {
		s.PassCount++
	}
}

// PassRate returns the current pass rate.
func (s *CurriculumState) PassRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.PassCount) / float64(s.Total)
}

// ShouldPromote returns true if the model should advance to harder tasks.
func (s *CurriculumState) ShouldPromote(cfg *CurriculumConfig) bool {
	return s.Total >= cfg.MinSamples && s.PassRate() >= cfg.PromotionThreshold && s.Level < DifficultyExtreme
}

// ShouldDemote returns true if the model should drop to easier tasks.
func (s *CurriculumState) ShouldDemote(cfg *CurriculumConfig) bool {
	return s.Total >= cfg.MinSamples && s.PassRate() < cfg.DemotionThreshold && s.Level > DifficultyEasy
}

// Promote advances to the next difficulty level and resets counters.
func (s *CurriculumState) Promote() {
	if s.Level < DifficultyExtreme {
		s.Level++
		s.PassCount = 0
		s.Total = 0
	}
}

// Demote drops to the previous difficulty level and resets counters.
func (s *CurriculumState) Demote() {
	if s.Level > DifficultyEasy {
		s.Level--
		s.PassCount = 0
		s.Total = 0
	}
}
