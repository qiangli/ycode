package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PromotionTracker tracks scenario pass history for automatic promotion
// from UsuallyPasses to AlwaysPasses (and demotion on failure).
//
// A scenario is promoted after ConsecutivePassesRequired consecutive runs
// where pass@k = 1.0. It is demoted after a single failure.
type PromotionTracker struct {
	dir     string
	history map[string]*ScenarioHistory
}

// ScenarioHistory tracks the pass/fail history of a single scenario.
type ScenarioHistory struct {
	Scenario          string    `json:"scenario"`
	Policy            string    `json:"policy"`
	ConsecutivePasses int       `json:"consecutive_passes"`
	TotalRuns         int       `json:"total_runs"`
	LastRunAt         time.Time `json:"last_run_at"`
	PromotedAt        time.Time `json:"promoted_at,omitzero"`
	DemotedAt         time.Time `json:"demoted_at,omitzero"`
}

const (
	// ConsecutivePassesRequired is the number of consecutive perfect runs
	// needed to promote from UsuallyPasses to AlwaysPasses.
	// Matches Gemini CLI's requirement of 7 stable nightly runs.
	ConsecutivePassesRequired = 7

	promotionFile = "promotion_history.json"
)

// NewPromotionTracker loads or creates a tracker at the given directory.
func NewPromotionTracker(dir string) (*PromotionTracker, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create promotion dir: %w", err)
	}

	pt := &PromotionTracker{
		dir:     dir,
		history: make(map[string]*ScenarioHistory),
	}

	// Load existing history.
	path := filepath.Join(dir, promotionFile)
	data, err := os.ReadFile(path)
	if err == nil {
		var entries []*ScenarioHistory
		if err := json.Unmarshal(data, &entries); err == nil {
			for _, e := range entries {
				pt.history[e.Scenario] = e
			}
		}
	}

	return pt, nil
}

// RecordRun records the result of a scenario run and returns any
// promotion or demotion events.
func (pt *PromotionTracker) RecordRun(scenarioName string, currentPolicy Policy, passAtK float64) *PromotionEvent {
	h, ok := pt.history[scenarioName]
	if !ok {
		h = &ScenarioHistory{
			Scenario: scenarioName,
			Policy:   currentPolicy.String(),
		}
		pt.history[scenarioName] = h
	}

	h.TotalRuns++
	h.LastRunAt = time.Now()

	perfect := passAtK >= 1.0

	if perfect {
		h.ConsecutivePasses++
	} else {
		h.ConsecutivePasses = 0
	}

	var event *PromotionEvent

	switch currentPolicy {
	case UsuallyPasses:
		// Check for promotion.
		if h.ConsecutivePasses >= ConsecutivePassesRequired {
			h.Policy = AlwaysPasses.String()
			h.PromotedAt = time.Now()
			h.ConsecutivePasses = 0 // reset counter
			event = &PromotionEvent{
				Scenario:   scenarioName,
				Action:     PromotionPromote,
				FromPolicy: UsuallyPasses,
				ToPolicy:   AlwaysPasses,
				Reason:     fmt.Sprintf("passed %d consecutive runs with pass@k=1.0", ConsecutivePassesRequired),
			}
		}

	case AlwaysPasses:
		// Check for demotion.
		if !perfect {
			h.Policy = UsuallyPasses.String()
			h.DemotedAt = time.Now()
			event = &PromotionEvent{
				Scenario:   scenarioName,
				Action:     PromotionDemote,
				FromPolicy: AlwaysPasses,
				ToPolicy:   UsuallyPasses,
				Reason:     fmt.Sprintf("failed with pass@k=%.2f", passAtK),
			}
		}
	}

	return event
}

// Save persists the promotion history to disk.
func (pt *PromotionTracker) Save() error {
	entries := make([]*ScenarioHistory, 0, len(pt.history))
	for _, h := range pt.history {
		entries = append(entries, h)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal promotion history: %w", err)
	}

	path := filepath.Join(pt.dir, promotionFile)
	return os.WriteFile(path, data, 0o644)
}

// Get returns the history for a scenario, or nil if not tracked.
func (pt *PromotionTracker) Get(scenarioName string) *ScenarioHistory {
	return pt.history[scenarioName]
}

// ReadyForPromotion returns scenarios that are close to promotion threshold.
func (pt *PromotionTracker) ReadyForPromotion() []*ScenarioHistory {
	var ready []*ScenarioHistory
	for _, h := range pt.history {
		if h.Policy == UsuallyPasses.String() && h.ConsecutivePasses >= ConsecutivePassesRequired-2 {
			ready = append(ready, h)
		}
	}
	return ready
}

// PromotionAction is the type of promotion event.
type PromotionAction int

const (
	PromotionPromote PromotionAction = iota
	PromotionDemote
)

func (a PromotionAction) String() string {
	if a == PromotionPromote {
		return "promote"
	}
	return "demote"
}

// PromotionEvent describes a policy change for a scenario.
type PromotionEvent struct {
	Scenario   string          `json:"scenario"`
	Action     PromotionAction `json:"action"`
	FromPolicy Policy          `json:"from_policy"`
	ToPolicy   Policy          `json:"to_policy"`
	Reason     string          `json:"reason"`
}

func (e *PromotionEvent) String() string {
	return fmt.Sprintf("%s: %s %s -> %s (%s)",
		e.Scenario, e.Action, e.FromPolicy, e.ToPolicy, e.Reason)
}
