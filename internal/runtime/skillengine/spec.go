// Package skillengine implements a self-evolving skill system.
// Skills auto-select based on context, track performance, and evolve
// through three modes: FIX (repair broken skills), DERIVED (specialize
// from successful deviations), and CAPTURED (extract new skills from
// recurring patterns in procedural memory).
package skillengine

import (
	"regexp"
	"time"
)

// SkillSpec defines a skill with metadata, triggers, and performance tracking.
type SkillSpec struct {
	// Identity
	Name    string `json:"name" yaml:"name"`
	Version int    `json:"version" yaml:"version"`
	Parent  string `json:"parent,omitempty" yaml:"parent,omitempty"` // lineage parent (for DERIVED/FIX)

	// Description and instruction
	Description string `json:"description" yaml:"description"`
	Instruction string `json:"instruction" yaml:"instruction"` // injected into system prompt

	// Trigger conditions for auto-activation
	TriggerPatterns []string `json:"trigger_patterns,omitempty" yaml:"trigger_patterns,omitempty"`
	TriggerKeywords []string `json:"trigger_keywords,omitempty" yaml:"trigger_keywords,omitempty"`

	// TelemetryTriggers are regex patterns matched against the
	// normalized error string of an observed tool failure. Used by
	// the selfheal Phase 6 loop: when a worker successfully fixes
	// a signature, the Learn callback captures a skill with the
	// signature's normalized error as a telemetry trigger so future
	// occurrences (on this or any other operator's machine running
	// the same skill registry) get matched and the prior fix
	// context is fed into the autoloop.
	//
	// Distinct from TriggerPatterns because user-text patterns and
	// telemetry-error patterns have completely different match
	// semantics (one is freeform prose; the other is structured,
	// normalized error output). Keeping them separate avoids
	// FindBestMatch confusion.
	TelemetryTriggers []string `json:"telemetry_triggers,omitempty" yaml:"telemetry_triggers,omitempty"`

	// Tool constraints (if set, only these tools are available)
	AllowedTools []string `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`

	// Quality criteria for success evaluation
	SuccessCriteria string `json:"success_criteria,omitempty" yaml:"success_criteria,omitempty"`

	// Compatibility constraints (provider filtering, trust level).
	Compatibility CompatibilitySpec `json:"compatibility,omitempty" yaml:"compatibility,omitempty"`

	// Evolution metadata
	EvolutionMode EvolutionMode `json:"evolution_mode,omitempty" yaml:"evolution_mode,omitempty"`
	CreatedAt     time.Time     `json:"created_at" yaml:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at" yaml:"updated_at"`

	// Performance stats (updated after each use)
	Stats SkillStats `json:"stats" yaml:"stats"`

	// Compiled trigger patterns (not serialized)
	compiledPatterns  []*regexp.Regexp
	compiledTelemetry []*regexp.Regexp
}

// MatchesTelemetry reports whether any TelemetryTriggers regex matches
// the given normalized error string. Returns false for skills with no
// telemetry triggers — they can still be selected via the user-text
// path (MatchScore).
func (s *SkillSpec) MatchesTelemetry(normalized string) bool {
	if len(s.TelemetryTriggers) == 0 || normalized == "" {
		return false
	}
	s.compileTelemetry()
	for _, re := range s.compiledTelemetry {
		if re.MatchString(normalized) {
			return true
		}
	}
	return false
}

func (s *SkillSpec) compileTelemetry() {
	if len(s.compiledTelemetry) == len(s.TelemetryTriggers) {
		return
	}
	s.compiledTelemetry = make([]*regexp.Regexp, 0, len(s.TelemetryTriggers))
	for _, p := range s.TelemetryTriggers {
		if re, err := regexp.Compile(p); err == nil {
			s.compiledTelemetry = append(s.compiledTelemetry, re)
		}
	}
}

// SkillStats tracks skill performance over time.
type SkillStats struct {
	Uses         int       `json:"uses"`
	Successes    int       `json:"successes"`
	Failures     int       `json:"failures"`
	AvgDuration  float64   `json:"avg_duration_ms"`
	SuccessRate  float64   `json:"success_rate"`
	LastUsed     time.Time `json:"last_used,omitempty"`
	DecayedScore float64   `json:"decayed_score"` // success rate with 5%/week decay
}

// EvolutionMode describes how a skill was created.
type EvolutionMode string

const (
	EvolutionOriginal EvolutionMode = "original" // manually created
	EvolutionFix      EvolutionMode = "fix"      // repaired from a broken skill
	EvolutionDerived  EvolutionMode = "derived"  // specialized from parent
	EvolutionCaptured EvolutionMode = "captured" // extracted from procedural memory
)

// MatchScore returns how well a skill matches the given text.
// Returns 0 if no match, higher values for better matches.
func (s *SkillSpec) MatchScore(text string) float64 {
	s.compilePatterns()

	var score float64

	// Check regex patterns.
	for _, re := range s.compiledPatterns {
		if re.MatchString(text) {
			score += 1.0
		}
	}

	// Check keyword matches.
	for _, kw := range s.TriggerKeywords {
		if containsWord(text, kw) {
			score += 0.5
		}
	}

	// Weight by historical success rate.
	if s.Stats.Uses > 0 {
		score *= (0.5 + 0.5*s.Stats.DecayedScore)
	}

	return score
}

func (s *SkillSpec) compilePatterns() {
	if len(s.compiledPatterns) == len(s.TriggerPatterns) {
		return
	}
	s.compiledPatterns = make([]*regexp.Regexp, 0, len(s.TriggerPatterns))
	for _, p := range s.TriggerPatterns {
		if re, err := regexp.Compile(p); err == nil {
			s.compiledPatterns = append(s.compiledPatterns, re)
		}
	}
}

// containsWord checks if text contains the word (case-insensitive substring).
func containsWord(text, word string) bool {
	// Simple case-insensitive contains.
	lower := func(s string) string {
		b := make([]byte, len(s))
		for i := range s {
			c := s[i]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			b[i] = c
		}
		return string(b)
	}
	return len(word) > 0 && len(text) >= len(word) &&
		indexOf(lower(text), lower(word)) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
