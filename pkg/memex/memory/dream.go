package memory

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// Dreamer performs background memory consolidation across sessions.
type Dreamer struct {
	manager  *Manager
	enabled  bool
	interval time.Duration
	logger   *slog.Logger

	// ConsolidationFunc is an optional LLM-backed consolidation function.
	// When set, it's called with the formatted prompt and should return
	// the LLM's decision (MERGE/KEEP_BEST/DELETE_REDUNDANT + optional merged content).
	ConsolidationFunc func(prompt string) (string, error)
}

// NewDreamer creates a new dreamer for memory consolidation.
func NewDreamer(manager *Manager, enabled bool) *Dreamer {
	return &Dreamer{
		manager:  manager,
		enabled:  enabled,
		interval: 30 * time.Minute,
		logger:   slog.Default(),
	}
}

// Start begins background memory consolidation.
// Runs one consolidation pass immediately, then repeats on interval.
func (d *Dreamer) Start(ctx context.Context) error {
	if !d.enabled {
		return nil
	}

	d.logger.Info("auto-dream started", "interval", d.interval)

	// Run once immediately so short CLI sessions get at least one pass.
	if err := d.consolidate(); err != nil {
		d.logger.Error("dream consolidation failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d.interval):
			if err := d.consolidate(); err != nil {
				d.logger.Error("dream consolidation failed", "error", err)
			}
		}
	}
}

// consolidate merges and prunes memories.
func (d *Dreamer) consolidate() error {
	memories, err := d.manager.All()
	if err != nil {
		return err
	}

	if len(memories) == 0 {
		return nil
	}

	d.logger.Info("dream: consolidating memories", "count", len(memories))

	// Phase 1: Remove stale memories.
	removed := 0
	var surviving []*Memory
	for _, mem := range memories {
		if IsStale(mem) {
			if err := d.manager.Forget(mem.Name); err != nil {
				d.logger.Warn("dream: failed to remove stale memory", "name", mem.Name, "error", err)
				surviving = append(surviving, mem)
				continue
			}
			removed++
		} else {
			surviving = append(surviving, mem)
		}
	}

	// Phase 2: Decay value scores on infrequently accessed memories.
	for _, mem := range surviving {
		oldValue := mem.EffectiveValue()
		DecayValue(mem, 30)
		if mem.ValueScore != oldValue && mem.ValueScore > 0 {
			_ = d.manager.Save(mem) // persist decayed value
		}
	}

	// Phase 3: Merge similar memories (grouped by type).
	merged := d.mergeSimilar(surviving)

	// Phase 4: Consolidate persona observations.
	d.consolidatePersona()

	d.logger.Info("dream: consolidation complete",
		"removed_stale", removed,
		"merged", merged)

	return nil
}

// consolidatePersona prunes and merges persona observations,
// and decays knowledge domain confidence for stale domains.
func (d *Dreamer) consolidatePersona() {
	if d.manager.globalStore == nil {
		return
	}

	personas, err := ListPersonas(d.manager.globalStore)
	if err != nil || len(personas) == 0 {
		return
	}

	for _, p := range personas {
		changed := false

		// Decay knowledge domains not demonstrated in 60+ days.
		if p.Knowledge != nil {
			cutoff := time.Now().AddDate(0, 0, -60)
			for i := range p.Knowledge.Domains {
				d := &p.Knowledge.Domains[i]
				if !d.LastDemonstrated.IsZero() && d.LastDemonstrated.Before(cutoff) {
					d.Confidence *= 0.8 // gradual decay
					changed = true
				}
			}
		}

		// Merge redundant observations using Jaccard similarity.
		if p.Interactions != nil && len(p.Interactions.Observations) > 1 {
			merged := mergeObservations(p.Interactions.Observations, 0.5)
			if len(merged) < len(p.Interactions.Observations) {
				p.Interactions.Observations = merged
				changed = true
			}
		}

		if changed {
			if err := SavePersona(d.manager.globalStore, p); err != nil {
				d.logger.Warn("dream: persona save failed", "id", p.ID, "error", err)
			}
		}
	}
}

// mergeObservations clusters similar observations and keeps the highest-confidence one from each cluster.
func mergeObservations(observations []PersonaObservation, threshold float64) []PersonaObservation {
	if len(observations) <= 1 {
		return observations
	}

	assigned := make([]bool, len(observations))
	var result []PersonaObservation

	for i := range observations {
		if assigned[i] {
			continue
		}
		assigned[i] = true
		best := observations[i]

		wordsI := wordSet(observations[i].Text)
		for j := i + 1; j < len(observations); j++ {
			if assigned[j] {
				continue
			}
			wordsJ := wordSet(observations[j].Text)
			if jaccardSimilarity(wordsI, wordsJ) >= threshold {
				assigned[j] = true
				if observations[j].Confidence > best.Confidence {
					best = observations[j]
				}
			}
		}
		result = append(result, best)
	}

	return result
}

// mergeSimilar finds and merges similar memories within each type.
func (d *Dreamer) mergeSimilar(memories []*Memory) int {
	merged := 0

	// Group by type first — only merge within the same type.
	byType := make(map[Type][]*Memory)
	for _, mem := range memories {
		byType[mem.Type] = append(byType[mem.Type], mem)
	}

	for _, typeMems := range byType {
		if len(typeMems) < 2 {
			continue
		}

		clusters := clusterBySimilarity(typeMems, 0.5)
		for _, cluster := range clusters {
			if len(cluster.Members) < 2 {
				continue
			}

			decision := d.decideCluster(cluster)
			count := d.applyDecision(cluster, decision)
			merged += count
		}
	}

	return merged
}

// clusterBySimilarity groups memories by word-overlap Jaccard similarity.
// threshold is the minimum similarity to join a cluster (0.0-1.0).
func clusterBySimilarity(memories []*Memory, threshold float64) []MemoryCluster {
	assigned := make([]bool, len(memories))
	var clusters []MemoryCluster

	for i := 0; i < len(memories); i++ {
		if assigned[i] {
			continue
		}

		cluster := MemoryCluster{
			Key:     memories[i].Name,
			Members: []*Memory{memories[i]},
		}
		assigned[i] = true

		wordsI := memoryWords(memories[i])
		for j := i + 1; j < len(memories); j++ {
			if assigned[j] {
				continue
			}
			wordsJ := memoryWords(memories[j])
			sim := jaccardSimilarity(wordsI, wordsJ)
			if sim >= threshold {
				cluster.Members = append(cluster.Members, memories[j])
				cluster.Similarity += sim
				assigned[j] = true
			}
		}

		if len(cluster.Members) > 1 {
			cluster.Similarity /= float64(len(cluster.Members) - 1)
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

// decideCluster determines what to do with a cluster of similar memories.
func (d *Dreamer) decideCluster(cluster MemoryCluster) ConsolidationDecision {
	// Try LLM-backed consolidation if available.
	if d.ConsolidationFunc != nil {
		prompt := FormatConsolidationPrompt(cluster.Members)
		response, err := d.ConsolidationFunc(prompt)
		if err == nil {
			decision := ParseConsolidationDecision(response)
			d.logger.Info("dream: LLM consolidation decision",
				"action", decision.Action,
				"cluster_size", len(cluster.Members),
			)
			return decision
		}
		d.logger.Warn("dream: LLM consolidation failed, using heuristic", "error", err)
	}

	// Heuristic fallback: keep the best by value score.
	return ConsolidationDecision{Action: "keep_best"}
}

// applyDecision executes a consolidation decision on a cluster.
// Returns the number of memories removed.
func (d *Dreamer) applyDecision(cluster MemoryCluster, decision ConsolidationDecision) int {
	removed := 0

	switch decision.Action {
	case "merge":
		if decision.Result == "" {
			// No merged content provided — fall through to keep_best.
			return d.applyDecision(cluster, ConsolidationDecision{Action: "keep_best"})
		}

		// Keep the first member as the merged survivor.
		survivor := cluster.Members[0]
		survivor.Content = decision.Result
		_ = d.manager.Save(survivor)

		// Remove all others.
		for _, mem := range cluster.Members[1:] {
			if err := d.manager.Forget(mem.Name); err != nil {
				d.logger.Warn("dream: merge cleanup failed", "name", mem.Name, "error", err)
				continue
			}
			removed++
		}

	case "keep_best":
		// Find the member with highest EffectiveValue.
		var best *Memory
		for _, mem := range cluster.Members {
			if best == nil || mem.EffectiveValue() > best.EffectiveValue() {
				best = mem
			}
		}
		// If values are tied, prefer newest.
		if best == nil {
			best = cluster.Members[0]
		}
		for _, mem := range cluster.Members {
			if mem == best {
				continue
			}
			// If values are equal, keep the newer one.
			if mem.EffectiveValue() == best.EffectiveValue() && mem.UpdatedAt.After(best.UpdatedAt) {
				if err := d.manager.Forget(best.Name); err == nil {
					removed++
				}
				best = mem
				continue
			}
			if err := d.manager.Forget(mem.Name); err != nil {
				d.logger.Warn("dream: keep_best cleanup failed", "name", mem.Name, "error", err)
				continue
			}
			removed++
		}

	case "delete_redundant":
		// Keep newest, remove the rest.
		var newest *Memory
		for _, mem := range cluster.Members {
			if newest == nil || mem.UpdatedAt.After(newest.UpdatedAt) {
				newest = mem
			}
		}
		for _, mem := range cluster.Members {
			if mem == newest {
				continue
			}
			if err := d.manager.Forget(mem.Name); err != nil {
				d.logger.Warn("dream: delete_redundant cleanup failed", "name", mem.Name, "error", err)
				continue
			}
			removed++
		}
	}

	return removed
}

// normalizeDescription creates a rough key for similarity matching.
func normalizeDescription(desc string) string {
	desc = strings.ToLower(desc)
	desc = strings.Join(strings.Fields(desc), " ")
	if len(desc) > 60 {
		desc = desc[:60]
	}
	return desc
}

// SetEnabled enables or disables the dreamer.
func (d *Dreamer) SetEnabled(enabled bool) {
	d.enabled = enabled
}

// IsEnabled returns whether the dreamer is enabled.
func (d *Dreamer) IsEnabled() bool {
	return d.enabled
}
