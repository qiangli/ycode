package reward

import "context"

// BinaryReward returns 1.0 if the check function passes, 0.0 otherwise.
type BinaryReward struct {
	Check func(result *AgentResult) bool
}

func (b *BinaryReward) Score(ctx context.Context, result *AgentResult) (float64, error) {
	if b.Check(result) {
		return 1.0, nil
	}
	return 0.0, nil
}
