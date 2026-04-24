package memory

import (
	"context"
	"fmt"
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
		return fmt.Errorf("list memories: %w", err)
	}

	if len(memories) == 0 {
		return nil
	}

	d.logger.Info("dream: consolidating memories", "count", len(memories))

	// Remove stale memories.
	removed := 0
	for _, mem := range memories {
		if IsStale(mem) {
			if err := d.manager.Forget(mem.Name); err != nil {
				d.logger.Warn("dream: failed to remove stale memory", "name", mem.Name, "error", err)
				continue
			}
			removed++
		}
	}

	// Merge similar project memories.
	merged := d.mergeSimilar(memories)

	d.logger.Info("dream: consolidation complete",
		"removed_stale", removed,
		"merged", merged)

	return nil
}

// mergeSimilar finds and merges project memories with similar descriptions.
func (d *Dreamer) mergeSimilar(memories []*Memory) int {
	merged := 0
	seen := make(map[string]*Memory)

	for _, mem := range memories {
		if mem.Type != TypeProject {
			continue
		}

		key := normalizeDescription(mem.Description)
		if existing, ok := seen[key]; ok {
			// Merge content into the newer memory.
			if mem.UpdatedAt.After(existing.UpdatedAt) {
				seen[key] = mem
				_ = d.manager.Forget(existing.Name)
			} else {
				_ = d.manager.Forget(mem.Name)
			}
			merged++
		} else {
			seen[key] = mem
		}
	}

	return merged
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
