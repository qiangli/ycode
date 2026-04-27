package swarm

import (
	"sync"

	"github.com/qiangli/ycode/internal/runtime/agentdef"
)

// TriggerRegistry manages keyword-triggered agent activation.
type TriggerRegistry struct {
	mu       sync.RWMutex
	agentDef *agentdef.Registry

	// Track trigger counts per turn to enforce MaxPerTurn.
	turnCounts map[string]int // agent name -> count this turn
}

// NewTriggerRegistry creates a new trigger registry.
func NewTriggerRegistry(defs *agentdef.Registry) *TriggerRegistry {
	return &TriggerRegistry{
		agentDef:   defs,
		turnCounts: make(map[string]int),
	}
}

// TriggerMatch represents a matched trigger with its agent definition.
type TriggerMatch struct {
	AgentDef *agentdef.AgentDefinition
}

// Check scans text for trigger patterns and returns matching agents.
// Respects MaxPerTurn limits.
func (tr *TriggerRegistry) Check(text string) []TriggerMatch {
	if tr.agentDef == nil {
		return nil
	}

	tr.mu.RLock()
	defer tr.mu.RUnlock()

	triggered := tr.agentDef.FindTriggered(text)
	var matches []TriggerMatch

	for _, def := range triggered {
		// Check per-turn limits.
		for _, tp := range def.Triggers {
			if tp.MaxPerTurn > 0 {
				count := tr.turnCounts[def.Name]
				if count >= tp.MaxPerTurn {
					continue
				}
			}
			matches = append(matches, TriggerMatch{AgentDef: def})
			break // one match per agent is enough
		}
	}

	return matches
}

// RecordTrigger increments the trigger count for an agent this turn.
func (tr *TriggerRegistry) RecordTrigger(agentName string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.turnCounts[agentName]++
}

// ResetTurn clears per-turn counters. Call at the start of each turn.
func (tr *TriggerRegistry) ResetTurn() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.turnCounts = make(map[string]int)
}
