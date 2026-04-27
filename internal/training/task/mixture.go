package task

import (
	"fmt"
	"math/rand/v2"
)

// Mixture combines multiple tasks with weighted sampling.
type Mixture struct {
	tasks   []Task
	weights []float64
	total   float64
}

// NewMixture creates a task mixture. Weights must be positive.
func NewMixture(tasks []Task, weights []float64) (*Mixture, error) {
	if len(tasks) != len(weights) {
		return nil, fmt.Errorf("tasks and weights must have equal length")
	}
	var total float64
	for _, w := range weights {
		if w <= 0 {
			return nil, fmt.Errorf("weights must be positive")
		}
		total += w
	}
	return &Mixture{tasks: tasks, weights: weights, total: total}, nil
}

func (m *Mixture) Name() string { return "mixture" }

func (m *Mixture) Len() int {
	total := 0
	for _, t := range m.tasks {
		total += t.Len()
	}
	return total
}

// Sample returns a random example weighted by task weights.
func (m *Mixture) Sample() (*Example, Task, error) {
	r := rand.Float64() * m.total
	cum := 0.0
	for i, w := range m.weights {
		cum += w
		if r < cum {
			idx := rand.IntN(m.tasks[i].Len())
			ex, err := m.tasks[i].GetExample(idx)
			return ex, m.tasks[i], err
		}
	}
	// Fallback to last task.
	last := m.tasks[len(m.tasks)-1]
	ex, err := last.GetExample(rand.IntN(last.Len()))
	return ex, last, err
}

func (m *Mixture) GetExample(index int) (*Example, error) {
	offset := 0
	for _, t := range m.tasks {
		if index < offset+t.Len() {
			return t.GetExample(index - offset)
		}
		offset += t.Len()
	}
	return nil, fmt.Errorf("index %d out of range", index)
}

func (m *Mixture) Evaluate(example *Example, completion string) (float64, error) {
	// Delegate to the task that owns this example.
	// Since we don't track ownership, use the first task that succeeds.
	for _, t := range m.tasks {
		score, err := t.Evaluate(example, completion)
		if err == nil && score > 0 {
			return score, nil
		}
	}
	return 0.0, nil
}
