package scratchpad

import (
	"fmt"
	"time"
)

// AutoCheckpointer triggers checkpoint saves on compaction events.
type AutoCheckpointer struct {
	checkpoints *CheckpointManager
	worklog     *WorkLog
	enabled     bool
}

// NewAutoCheckpointer creates an auto-checkpointer.
func NewAutoCheckpointer(checkpoints *CheckpointManager, worklog *WorkLog, enabled bool) *AutoCheckpointer {
	return &AutoCheckpointer{
		checkpoints: checkpoints,
		worklog:     worklog,
		enabled:     enabled,
	}
}

// OnCompaction is called when session compaction occurs.
// It saves a checkpoint with the compaction summary and logs the event.
func (ac *AutoCheckpointer) OnCompaction(sessionID string, summary string, compactedCount int) error {
	if !ac.enabled {
		return nil
	}

	id := fmt.Sprintf("compact_%s_%d", sessionID, time.Now().Unix())
	label := fmt.Sprintf("Auto-checkpoint on compaction (%d messages compacted)", compactedCount)

	data := map[string]any{
		"type":            "compaction",
		"session_id":      sessionID,
		"compacted_count": compactedCount,
		"summary":         summary,
		"timestamp":       time.Now().Format(time.RFC3339),
	}

	if err := ac.checkpoints.Save(id, label, data); err != nil {
		return fmt.Errorf("auto-checkpoint save: %w", err)
	}

	if ac.worklog != nil {
		_ = ac.worklog.Append(fmt.Sprintf("Auto-checkpoint saved: %s (%d messages compacted)", id, compactedCount))
	}

	return nil
}

// IsEnabled returns whether auto-checkpointing is enabled.
func (ac *AutoCheckpointer) IsEnabled() bool {
	return ac.enabled
}

// SetEnabled enables or disables auto-checkpointing.
func (ac *AutoCheckpointer) SetEnabled(enabled bool) {
	ac.enabled = enabled
}
