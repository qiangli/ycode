// Package tools — cooccurrence tracks directional tool co-occurrence patterns
// to enable cluster-based pre-activation. When tool A frequently leads to tools
// B and C being used in the same session, activating A should also activate B
// and C. But if B is typically a terminal tool (used alone or as the final tool),
// activating B should NOT activate A or C.
//
// The relationship is directional: A→B means "when A is used, B follows".
// This captures workflow patterns like:
//   - grep_search → read_file → edit_file (exploration→reading→editing)
//   - git_status → git_add → git_commit (staging workflow)
//   - Agent → AgentWait → AgentList (parallel orchestration)
package tools

import (
	"sort"
	"sync"
)

// CoOccurrence tracks directional tool co-occurrence within sessions.
type CoOccurrence struct {
	mu sync.RWMutex

	// forward[A][B] = count of times B was used after A in the same session.
	forward map[string]map[string]int

	// terminal[A] = count of times A was the last tool used in a session.
	terminal map[string]int

	// totalSessions[A] = count of sessions where A appeared.
	totalSessions map[string]int

	// minSupport is the minimum co-occurrence count to be considered significant.
	minSupport int

	// minConfidence is the minimum P(B|A) to include B as a co-activated tool.
	minConfidence float64
}

// NewCoOccurrence creates a co-occurrence tracker.
func NewCoOccurrence() *CoOccurrence {
	return &CoOccurrence{
		forward:       make(map[string]map[string]int),
		terminal:      make(map[string]int),
		totalSessions: make(map[string]int),
		minSupport:    3,   // need at least 3 co-occurrences
		minConfidence: 0.3, // B must follow A in at least 30% of A's sessions
	}
}

// SetThresholds configures the minimum support and confidence thresholds.
func (co *CoOccurrence) SetThresholds(minSupport int, minConfidence float64) {
	co.mu.Lock()
	defer co.mu.Unlock()
	if minSupport > 0 {
		co.minSupport = minSupport
	}
	if minConfidence > 0 && minConfidence <= 1.0 {
		co.minConfidence = minConfidence
	}
}

// RecordSession records a sequence of tool calls from one session/turn.
// The order matters: tools[0] was called first, tools[N-1] last.
// Duplicate consecutive tools are collapsed (A,A,B → A,B).
func (co *CoOccurrence) RecordSession(tools []string) {
	if len(tools) == 0 {
		return
	}

	// Deduplicate consecutive runs.
	deduped := []string{tools[0]}
	for i := 1; i < len(tools); i++ {
		if tools[i] != tools[i-1] {
			deduped = append(deduped, tools[i])
		}
	}

	co.mu.Lock()
	defer co.mu.Unlock()

	// Track which tools appeared in this session (for totalSessions).
	seen := make(map[string]bool)

	// Record forward edges: for each tool, all tools that come after it.
	for i, tool := range deduped {
		if !seen[tool] {
			co.totalSessions[tool]++
			seen[tool] = true
		}

		// Record forward co-occurrence with every tool that follows.
		for j := i + 1; j < len(deduped); j++ {
			follower := deduped[j]
			if co.forward[tool] == nil {
				co.forward[tool] = make(map[string]int)
			}
			co.forward[tool][follower]++
		}
	}

	// Record the last tool as terminal.
	co.terminal[deduped[len(deduped)-1]]++
}

// coActivationScore represents a candidate tool for co-activation.
type coActivationScore struct {
	Name       string
	Support    int     // absolute co-occurrence count
	Confidence float64 // P(follower | trigger)
}

// CoActivate returns tools that should be co-activated when triggerTool is
// activated. Returns up to maxResults tools, sorted by confidence (descending).
// Only tools exceeding both minSupport and minConfidence thresholds are returned.
func (co *CoOccurrence) CoActivate(triggerTool string, maxResults int) []string {
	co.mu.RLock()
	defer co.mu.RUnlock()

	followers, ok := co.forward[triggerTool]
	if !ok {
		return nil
	}

	totalA := co.totalSessions[triggerTool]
	if totalA == 0 {
		return nil
	}

	var candidates []coActivationScore
	for follower, count := range followers {
		if count < co.minSupport {
			continue
		}

		confidence := float64(count) / float64(totalA)
		if confidence < co.minConfidence {
			continue
		}

		// Check if the follower is primarily terminal — if the follower
		// is used as the last tool in >70% of its sessions, it's a
		// terminal tool and should not trigger further co-activation.
		// But it's fine to BE co-activated by another tool.
		candidates = append(candidates, coActivationScore{
			Name:       follower,
			Support:    count,
			Confidence: confidence,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})

	if maxResults <= 0 {
		maxResults = 5
	}
	var result []string
	for _, c := range candidates {
		if len(result) >= maxResults {
			break
		}
		result = append(result, c.Name)
	}
	return result
}

// IsTerminal returns true if a tool is predominantly used as the final tool
// in a session (terminal rate > 0.7). Terminal tools should not trigger
// co-activation of other tools when they are the activation trigger.
func (co *CoOccurrence) IsTerminal(toolName string) bool {
	co.mu.RLock()
	defer co.mu.RUnlock()

	total := co.totalSessions[toolName]
	if total < 3 { // need minimum sample
		return false
	}

	termCount := co.terminal[toolName]
	return float64(termCount)/float64(total) > 0.7
}

// Stats returns the co-occurrence statistics for a tool (for debugging/inspection).
func (co *CoOccurrence) Stats(toolName string) map[string]int {
	co.mu.RLock()
	defer co.mu.RUnlock()

	result := make(map[string]int)
	if followers, ok := co.forward[toolName]; ok {
		for k, v := range followers {
			result[k] = v
		}
	}
	return result
}

// AllClusters returns all discovered tool clusters (tools with co-activations
// that meet the thresholds). Useful for diagnostics.
func (co *CoOccurrence) AllClusters() map[string][]string {
	co.mu.RLock()
	defer co.mu.RUnlock()

	clusters := make(map[string][]string)
	for tool := range co.forward {
		// Don't create clusters for terminal tools.
		total := co.totalSessions[tool]
		if total == 0 {
			continue
		}
		termCount := co.terminal[tool]
		if total >= 3 && float64(termCount)/float64(total) > 0.7 {
			continue // terminal tool, skip
		}

		followers := co.CoActivateInternal(tool, 5)
		if len(followers) > 0 {
			clusters[tool] = followers
		}
	}
	return clusters
}

// CoActivateInternal is like CoActivate but callable under read lock (no re-lock).
func (co *CoOccurrence) CoActivateInternal(triggerTool string, maxResults int) []string {
	followers, ok := co.forward[triggerTool]
	if !ok {
		return nil
	}

	totalA := co.totalSessions[triggerTool]
	if totalA == 0 {
		return nil
	}

	var candidates []coActivationScore
	for follower, count := range followers {
		if count < co.minSupport {
			continue
		}
		confidence := float64(count) / float64(totalA)
		if confidence < co.minConfidence {
			continue
		}
		candidates = append(candidates, coActivationScore{
			Name:       follower,
			Support:    count,
			Confidence: confidence,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Confidence > candidates[j].Confidence
	})

	if maxResults <= 0 {
		maxResults = 5
	}
	var result []string
	for _, c := range candidates {
		if len(result) >= maxResults {
			break
		}
		result = append(result, c.Name)
	}
	return result
}
