package memory

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
)

// PersonaResolver manages loading, matching, and creating personas.
type PersonaResolver struct {
	store  *Store
	logger *slog.Logger
}

// NewPersonaResolver creates a resolver backed by the given memory store.
// The store should be the global store (~/.agents/ycode/memory/).
func NewPersonaResolver(store *Store, logger *slog.Logger) *PersonaResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &PersonaResolver{
		store:  store,
		logger: logger,
	}
}

// Resolve finds the best-matching persona for the current environment,
// or creates a new one if no match exceeds the confidence threshold.
// It also handles migration from legacy UserProfile.
func (pr *PersonaResolver) Resolve(env *EnvironmentSignals) (*Persona, error) {
	personas, err := ListPersonas(pr.store)
	if err != nil {
		return nil, fmt.Errorf("list personas: %w", err)
	}

	// Find best match.
	var bestPersona *Persona
	var bestScore float64
	for _, p := range personas {
		score := matchScore(p.Environment, env)
		if score > bestScore {
			bestScore = score
			bestPersona = p
		}
	}

	if bestPersona != nil && bestScore >= 0.3 {
		bestPersona.Confidence = bestScore
		bestPersona.SessionContext = NewSessionContext()
		pr.logger.Info("persona resolved",
			"id", bestPersona.ID,
			"hint", bestPersona.DisplayHint,
			"confidence", bestScore,
		)
		return bestPersona, nil
	}

	// No match — check for legacy profile migration.
	id := envHash(env)
	migrated, err := MigrateProfile(pr.store, env, id)
	if err != nil {
		pr.logger.Warn("profile migration failed", "error", err)
	}
	if migrated != nil {
		migrated.SessionContext = NewSessionContext()
		if err := SavePersona(pr.store, migrated); err != nil {
			return nil, fmt.Errorf("save migrated persona: %w", err)
		}
		pr.logger.Info("persona migrated from profile",
			"id", migrated.ID,
			"hint", migrated.DisplayHint,
		)
		return migrated, nil
	}

	// Create new persona.
	p := NewPersona(id, env)
	p.SessionContext = NewSessionContext()
	if err := SavePersona(pr.store, p); err != nil {
		return nil, fmt.Errorf("save new persona: %w", err)
	}
	pr.logger.Info("new persona created",
		"id", p.ID,
		"hint", p.DisplayHint,
	)
	return p, nil
}

// matchScore computes a similarity score between stored and current
// environment signals. Returns a value in [0.0, 1.0].
func matchScore(stored, current *EnvironmentSignals) float64 {
	if stored == nil || current == nil {
		return 0
	}

	score := 0.0
	if stored.GitUserName != "" && stored.GitUserName == current.GitUserName {
		score += 0.35
	}
	if stored.GitEmail != "" && stored.GitEmail == current.GitEmail {
		score += 0.30
	}
	if stored.HomeDir != "" && stored.HomeDir == current.HomeDir {
		score += 0.15
	}
	if stored.Platform != "" && stored.Platform == current.Platform {
		score += 0.10
	}
	if stored.Shell != "" && stored.Shell == current.Shell {
		score += 0.05
	}
	if stored.Hostname != "" && stored.Hostname == current.Hostname {
		score += 0.05
	}

	return score
}

// envHash produces a stable ID from environment signals.
func envHash(env *EnvironmentSignals) string {
	// Use the strongest identity signals.
	parts := []string{
		env.GitUserName,
		env.GitEmail,
		env.HomeDir,
	}
	data := strings.Join(parts, "|")
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h[:8])
}
