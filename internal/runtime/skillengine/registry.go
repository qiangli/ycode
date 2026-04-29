package skillengine

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Registry manages skill definitions with versioning and performance tracking.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*SkillSpec // name -> latest version
	dir    string                // persistence directory
	logger *slog.Logger
}

// NewRegistry creates a skill registry backed by the given directory.
func NewRegistry(dir string) *Registry {
	return &Registry{
		skills: make(map[string]*SkillSpec),
		dir:    dir,
		logger: slog.Default(),
	}
}

// LoadFromDir loads all skill specs from .json files in the directory.
func (r *Registry) LoadFromDir() error {
	if r.dir == "" {
		return nil
	}

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read skills dir: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(r.dir, entry.Name()))
		if err != nil {
			r.logger.Warn("skillengine: failed to read skill", "file", entry.Name(), "error", err)
			continue
		}

		var spec SkillSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			r.logger.Warn("skillengine: failed to parse skill", "file", entry.Name(), "error", err)
			continue
		}

		r.skills[spec.Name] = &spec
	}

	r.logger.Info("skillengine: loaded skills", "count", len(r.skills))
	return nil
}

// Register adds or updates a skill in the registry.
func (r *Registry) Register(spec *SkillSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now()
	}
	spec.UpdatedAt = time.Now()

	r.mu.Lock()
	r.skills[spec.Name] = spec
	r.mu.Unlock()

	return r.persist(spec)
}

// Get returns a skill by name.
func (r *Registry) Get(name string) (*SkillSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// FindBestMatch returns the skill that best matches the given text.
// Returns nil if no skill scores above the threshold (0.5).
func (r *Registry) FindBestMatch(text string) *SkillSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var best *SkillSpec
	var bestScore float64

	for _, s := range r.skills {
		score := s.MatchScore(text)
		if score > bestScore && score >= 0.5 {
			best = s
			bestScore = score
		}
	}

	return best
}

// RecordOutcome updates a skill's performance statistics.
func (r *Registry) RecordOutcome(name string, success bool, durationMs float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.skills[name]
	if !ok {
		return
	}

	s.Stats.Uses++
	if success {
		s.Stats.Successes++
	} else {
		s.Stats.Failures++
	}

	// Running average of duration.
	if s.Stats.Uses == 1 {
		s.Stats.AvgDuration = durationMs
	} else {
		s.Stats.AvgDuration = s.Stats.AvgDuration*0.9 + durationMs*0.1
	}

	s.Stats.SuccessRate = float64(s.Stats.Successes) / float64(s.Stats.Uses)
	s.Stats.LastUsed = time.Now()
	s.Stats.DecayedScore = s.Stats.SuccessRate // decay applied in ApplyDecay

	// Copy for safe async persistence.
	cp := *s
	go r.persist(&cp)
}

// ApplyDecay reduces skill scores by 5% per week since last use.
func (r *Registry) ApplyDecay() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for _, s := range r.skills {
		if s.Stats.LastUsed.IsZero() || s.Stats.Uses == 0 {
			continue
		}
		weeksSinceUse := now.Sub(s.Stats.LastUsed).Hours() / (24 * 7)
		if weeksSinceUse > 0 {
			decay := 1.0 - 0.05*weeksSinceUse
			if decay < 0.1 {
				decay = 0.1 // floor at 10%
			}
			s.Stats.DecayedScore = s.Stats.SuccessRate * decay
		}
	}
}

// List returns all skills sorted by decayed score (descending).
func (r *Registry) List() []*SkillSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()

	specs := make([]*SkillSpec, 0, len(r.skills))
	for _, s := range r.skills {
		specs = append(specs, s)
	}
	sort.Slice(specs, func(i, j int) bool {
		return specs[i].Stats.DecayedScore > specs[j].Stats.DecayedScore
	})
	return specs
}

// persist saves a skill spec to disk.
func (r *Registry) persist(spec *SkillSpec) error {
	if r.dir == "" {
		return nil
	}

	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s_v%d.json", spec.Name, spec.Version)
	return os.WriteFile(filepath.Join(r.dir, filename), data, 0o644)
}
