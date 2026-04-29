package skillengine

import (
	"log/slog"
	"time"
)

// Engine is the core skill selection and execution wrapper.
// Before each conversation turn, it finds the best matching skill and
// injects its instruction into the system prompt.
type Engine struct {
	registry *Registry
	logger   *slog.Logger
}

// NewEngine creates a skill engine backed by the given registry.
func NewEngine(registry *Registry) *Engine {
	return &Engine{
		registry: registry,
		logger:   slog.Default(),
	}
}

// Registry returns the underlying skill registry.
func (e *Engine) Registry() *Registry { return e.registry }

// SelectSkill finds the best matching skill for the given user message.
// Returns nil if no skill is a good match.
func (e *Engine) SelectSkill(userMessage string) *SkillSpec {
	skill := e.registry.FindBestMatch(userMessage)
	if skill != nil {
		e.logger.Info("skillengine: auto-selected skill",
			"skill", skill.Name,
			"version", skill.Version,
		)
	}
	return skill
}

// RecordResult records the outcome of a skill execution and triggers evolution if needed.
func (e *Engine) RecordResult(skillName string, success bool, durationMs float64) {
	e.registry.RecordOutcome(skillName, success, durationMs)

	// Check if the skill needs evolution.
	skill, ok := e.registry.Get(skillName)
	if !ok {
		return
	}

	// If the skill has failed 3+ times with <50% success rate, flag for FIX.
	if skill.Stats.Failures >= 3 && skill.Stats.SuccessRate < 0.5 {
		e.logger.Warn("skillengine: skill degraded, flagged for FIX evolution",
			"skill", skillName,
			"success_rate", skill.Stats.SuccessRate,
			"failures", skill.Stats.Failures,
		)
	}
}

// ApplyDecay applies weekly decay to all skill scores.
func (e *Engine) ApplyDecay() {
	e.registry.ApplyDecay()
}

// SkillExecution tracks an in-progress skill execution.
type SkillExecution struct {
	Skill   *SkillSpec
	Started time.Time
}

// NewExecution starts tracking a skill execution.
func NewExecution(skill *SkillSpec) *SkillExecution {
	return &SkillExecution{
		Skill:   skill,
		Started: time.Now(),
	}
}

// Elapsed returns the duration since the execution started.
func (se *SkillExecution) Elapsed() time.Duration {
	return time.Since(se.Started)
}
