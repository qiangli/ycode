package skillengine

import (
	"fmt"
	"log/slog"
	"time"
)

// Evolver handles skill evolution through three modes:
// - FIX: repair a broken/underperforming skill
// - DERIVED: create a specialized variant from a successful deviation
// - CAPTURED: extract a new skill from recurring procedural patterns
type Evolver struct {
	registry *Registry
	logger   *slog.Logger
}

// NewEvolver creates a skill evolver.
func NewEvolver(registry *Registry) *Evolver {
	return &Evolver{
		registry: registry,
		logger:   slog.Default(),
	}
}

// FixSkill creates a patched version of a skill with updated instructions.
// The original skill is preserved; the new version gets priority.
func (ev *Evolver) FixSkill(originalName, fixedInstruction, reason string) (*SkillSpec, error) {
	original, ok := ev.registry.Get(originalName)
	if !ok {
		return nil, fmt.Errorf("skill %q not found", originalName)
	}

	fixed := &SkillSpec{
		Name:            original.Name,
		Version:         original.Version + 1,
		Parent:          fmt.Sprintf("%s_v%d", original.Name, original.Version),
		Description:     original.Description,
		Instruction:     fixedInstruction,
		TriggerPatterns: original.TriggerPatterns,
		TriggerKeywords: original.TriggerKeywords,
		AllowedTools:    original.AllowedTools,
		SuccessCriteria: original.SuccessCriteria,
		EvolutionMode:   EvolutionFix,
		CreatedAt:       time.Now(),
	}

	if err := ev.registry.Register(fixed); err != nil {
		return nil, fmt.Errorf("register fixed skill: %w", err)
	}

	ev.logger.Info("skillengine: FIX evolution",
		"skill", originalName,
		"version", fixed.Version,
		"reason", reason,
	)

	return fixed, nil
}

// DeriveSkill creates a specialized variant from a parent skill.
// Used when the agent deviated from a skill's instructions but still succeeded.
func (ev *Evolver) DeriveSkill(parentName, derivedInstruction, specialization string) (*SkillSpec, error) {
	parent, ok := ev.registry.Get(parentName)
	if !ok {
		return nil, fmt.Errorf("parent skill %q not found", parentName)
	}

	derived := &SkillSpec{
		Name:            fmt.Sprintf("%s_%s", parent.Name, sanitize(specialization)),
		Version:         1,
		Parent:          parent.Name,
		Description:     fmt.Sprintf("%s (specialized: %s)", parent.Description, specialization),
		Instruction:     derivedInstruction,
		TriggerPatterns: parent.TriggerPatterns,
		TriggerKeywords: append(parent.TriggerKeywords, specialization),
		AllowedTools:    parent.AllowedTools,
		SuccessCriteria: parent.SuccessCriteria,
		EvolutionMode:   EvolutionDerived,
		CreatedAt:       time.Now(),
	}

	if err := ev.registry.Register(derived); err != nil {
		return nil, fmt.Errorf("register derived skill: %w", err)
	}

	ev.logger.Info("skillengine: DERIVED evolution",
		"parent", parentName,
		"derived", derived.Name,
		"specialization", specialization,
	)

	return derived, nil
}

// CaptureSkill extracts a new skill from a recurring pattern.
// Used by the Learner when it detects procedural patterns accumulating.
func (ev *Evolver) CaptureSkill(name, description, instruction string, keywords []string) (*SkillSpec, error) {
	captured := &SkillSpec{
		Name:            name,
		Version:         1,
		Description:     description,
		Instruction:     instruction,
		TriggerKeywords: keywords,
		EvolutionMode:   EvolutionCaptured,
		CreatedAt:       time.Now(),
	}

	if err := ev.registry.Register(captured); err != nil {
		return nil, fmt.Errorf("register captured skill: %w", err)
	}

	ev.logger.Info("skillengine: CAPTURED evolution",
		"skill", name,
		"keywords", keywords,
	)

	return captured, nil
}

// CheckForRollback compares a skill version against its parent.
// If the newer version has worse performance after minimum uses, revert.
func (ev *Evolver) CheckForRollback(skillName string, minUses int) bool {
	skill, ok := ev.registry.Get(skillName)
	if !ok || skill.Parent == "" || skill.Stats.Uses < minUses {
		return false
	}

	parent, ok := ev.registry.Get(skill.Parent)
	if !ok {
		return false
	}

	// Rollback if the evolved version is significantly worse.
	if skill.Stats.SuccessRate < parent.Stats.SuccessRate-0.1 {
		ev.logger.Warn("skillengine: rolling back to parent",
			"skill", skillName,
			"skill_rate", skill.Stats.SuccessRate,
			"parent_rate", parent.Stats.SuccessRate,
		)
		// Re-register parent as current.
		parent.Version = skill.Version + 1
		parent.UpdatedAt = time.Now()
		_ = ev.registry.Register(parent)
		return true
	}

	return false
}

// sanitize creates a safe name component from a string.
func sanitize(s string) string {
	result := make([]byte, 0, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+'a'-'A')
		} else if c == ' ' || c == '-' {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "unnamed"
	}
	return string(result)
}
