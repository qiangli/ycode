package rollout

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CollectorConfig configures trajectory collection.
type CollectorConfig struct {
	OutputPath  string // JSONL output file
	Concurrency int    // parallel rollouts
}

// SaveTrajectories writes scored trajectories to a JSONL file.
func SaveTrajectories(path string, trajectories []ScoredTrajectory) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, t := range trajectories {
		if err := enc.Encode(t); err != nil {
			return fmt.Errorf("encode trajectory: %w", err)
		}
	}
	return nil
}

// LoadTrajectories reads scored trajectories from a JSONL file.
func LoadTrajectories(path string) ([]ScoredTrajectory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var trajectories []ScoredTrajectory
	dec := json.NewDecoder(strings.NewReader(string(data)))
	for dec.More() {
		var t ScoredTrajectory
		if err := dec.Decode(&t); err != nil {
			return nil, fmt.Errorf("decode trajectory: %w", err)
		}
		trajectories = append(trajectories, t)
	}
	return trajectories, nil
}

// TrajectoryStats computes aggregate statistics from trajectories.
type TrajectoryStats struct {
	Total       int     `json:"total"`
	AvgScore    float64 `json:"avg_score"`
	MaxScore    float64 `json:"max_score"`
	MinScore    float64 `json:"min_score"`
	AvgTurns    float64 `json:"avg_turns"`
	TotalErrors int     `json:"total_errors"`
	PassRate    float64 `json:"pass_rate"` // fraction with score >= 1.0
}

// ComputeStats calculates aggregate statistics.
func ComputeStats(trajectories []ScoredTrajectory) TrajectoryStats {
	if len(trajectories) == 0 {
		return TrajectoryStats{}
	}
	stats := TrajectoryStats{
		Total:    len(trajectories),
		MinScore: trajectories[0].Score,
	}
	passes := 0
	for _, t := range trajectories {
		stats.AvgScore += t.Score
		stats.AvgTurns += float64(t.TurnsUsed)
		stats.TotalErrors += t.ToolErrors
		if t.Score > stats.MaxScore {
			stats.MaxScore = t.Score
		}
		if t.Score < stats.MinScore {
			stats.MinScore = t.Score
		}
		if t.Score >= 1.0 {
			passes++
		}
	}
	stats.AvgScore /= float64(len(trajectories))
	stats.AvgTurns /= float64(len(trajectories))
	stats.PassRate = float64(passes) / float64(len(trajectories))
	return stats
}
