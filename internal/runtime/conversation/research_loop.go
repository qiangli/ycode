package conversation

import (
	"context"
	"fmt"
	"strings"
)

// ResearchLoopConfig controls the auto-research loop behavior.
type ResearchLoopConfig struct {
	MaxDepth    int // maximum decompose-execute-synthesize cycles (default 2)
	MaxBreadth  int // maximum sub-queries per decomposition (default 10)
	MaxParallel int // maximum concurrent tasks (default 4)
}

// DefaultResearchLoopConfig returns sensible defaults.
func DefaultResearchLoopConfig() *ResearchLoopConfig {
	return &ResearchLoopConfig{
		MaxDepth:    2,
		MaxBreadth:  10,
		MaxParallel: 4,
	}
}

// ResearchLoop implements an autonomous explore-synthesize-identify-gaps-explore loop.
type ResearchLoop struct {
	Config *ResearchLoopConfig

	// Decompose breaks a query into sub-tasks. If nil, uses string-based fallback.
	Decompose func(ctx context.Context, query string) (*ResearchPlanV2, error)

	// RunTask executes a single research task.
	RunTask func(ctx context.Context, task *ResearchTask) (string, error)

	// IdentifyGaps analyzes synthesis results and returns follow-up queries.
	// If nil or returns empty, the loop stops.
	IdentifyGaps func(ctx context.Context, query, synthesis string) ([]string, error)
}

// Run executes the research loop: decompose → execute → synthesize → identify gaps → repeat.
func (rl *ResearchLoop) Run(ctx context.Context, query string) (string, error) {
	cfg := rl.Config
	if cfg == nil {
		cfg = DefaultResearchLoopConfig()
	}

	var allSyntheses []string
	currentQuery := query

	for depth := 0; depth < cfg.MaxDepth; depth++ {
		select {
		case <-ctx.Done():
			return strings.Join(allSyntheses, "\n\n---\n\n"), ctx.Err()
		default:
		}

		// Decompose.
		var plan *ResearchPlanV2
		var err error
		if rl.Decompose != nil {
			plan, err = rl.Decompose(ctx, currentQuery)
		}
		if plan == nil || err != nil {
			// Fallback to basic decomposition.
			plan = fallbackDecompose(currentQuery)
		}

		// Limit breadth.
		if len(plan.Tasks) > cfg.MaxBreadth {
			plan.Tasks = plan.Tasks[:cfg.MaxBreadth]
		}

		// Execute.
		executor := &ResearchExecutor{
			RunTask:     rl.RunTask,
			MaxParallel: cfg.MaxParallel,
		}
		results, err := executor.Execute(ctx, plan)
		if err != nil {
			return strings.Join(allSyntheses, "\n\n---\n\n"), fmt.Errorf("depth %d: %w", depth, err)
		}

		// Synthesize.
		synthesis := Synthesize(plan, results)
		allSyntheses = append(allSyntheses, synthesis)

		// Identify gaps for next iteration.
		if rl.IdentifyGaps == nil {
			break
		}
		gaps, err := rl.IdentifyGaps(ctx, query, synthesis)
		if err != nil || len(gaps) == 0 {
			break
		}

		// Prepare next query from gaps.
		currentQuery = "Follow-up research needed:\n" + strings.Join(gaps, "\n")
	}

	return strings.Join(allSyntheses, "\n\n---\n\n"), nil
}

// fallbackDecompose uses the basic string-splitting approach from research.go.
func fallbackDecompose(query string) *ResearchPlanV2 {
	basicPlan := NewResearchPlan(query)
	v2 := NewResearchPlanV2(query)
	for _, t := range basicPlan.Tasks {
		v2.AddTask(t.ID, t.Query, t.AgentType, nil)
	}
	return v2
}
