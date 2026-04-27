package reward

import (
	"context"
	"math"
)

// Signal is a named component of a multi-signal reward.
type Signal struct {
	Name   string
	Weight float64
	Scorer func(result *AgentResult) float64
}

// MultiSignalReward combines multiple weighted signals.
type MultiSignalReward struct {
	Signals []Signal
}

func (m *MultiSignalReward) Score(ctx context.Context, result *AgentResult) (float64, error) {
	var totalWeight float64
	var weightedSum float64
	for _, s := range m.Signals {
		score := s.Scorer(result)
		weightedSum += s.Weight * score
		totalWeight += s.Weight
	}
	if totalWeight == 0 {
		return 0.0, nil
	}
	return math.Min(1.0, math.Max(0.0, weightedSum/totalWeight)), nil
}

// ToolUsageSignal returns 1.0 if any of the specified tools were used.
func ToolUsageSignal(toolNames ...string) func(*AgentResult) float64 {
	nameSet := make(map[string]bool, len(toolNames))
	for _, n := range toolNames {
		nameSet[n] = true
	}
	return func(result *AgentResult) float64 {
		for _, msg := range result.Messages {
			for _, tc := range msg.ToolCalls {
				if nameSet[tc.Name] {
					return 1.0
				}
			}
		}
		return 0.0
	}
}

// EfficiencySignal penalizes excessive tool calls.
func EfficiencySignal(idealCalls, maxCalls int) func(*AgentResult) float64 {
	return func(result *AgentResult) float64 {
		count := 0
		for _, msg := range result.Messages {
			count += len(msg.ToolCalls)
		}
		if count <= idealCalls {
			return 1.0
		}
		if count >= maxCalls {
			return 0.0
		}
		return 1.0 - float64(count-idealCalls)/float64(maxCalls-idealCalls)
	}
}
