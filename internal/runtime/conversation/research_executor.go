package conversation

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// ResearchExecutor runs a ResearchPlanV2, respecting dependencies and
// executing independent tasks in parallel.
type ResearchExecutor struct {
	// RunTask executes a single research task and returns the result.
	RunTask func(ctx context.Context, task *ResearchTask) (string, error)

	// MaxParallel is the maximum number of concurrent tasks (default 4).
	MaxParallel int
}

// Execute runs all tasks in the plan, respecting the dependency DAG.
// Returns the collected results keyed by task ID.
func (re *ResearchExecutor) Execute(ctx context.Context, plan *ResearchPlanV2) (map[string]string, error) {
	if re.RunTask == nil {
		return nil, fmt.Errorf("RunTask function not set")
	}
	maxP := re.MaxParallel
	if maxP <= 0 {
		maxP = 4
	}

	results := make(map[string]string)
	var mu sync.Mutex

	for {
		ready := plan.Ready()
		if len(ready) == 0 {
			// Check if all done or stuck.
			if plan.IsComplete() {
				break
			}
			// Stuck — some tasks are pending but none are ready.
			return results, fmt.Errorf("deadlock: pending tasks with unmet dependencies")
		}

		// Limit concurrency.
		if len(ready) > maxP {
			ready = ready[:maxP]
		}

		// Mark as in_progress.
		for _, t := range ready {
			t.Status = "in_progress"
		}

		// Execute in parallel.
		var wg sync.WaitGroup
		errs := make([]error, len(ready))

		for i, task := range ready {
			wg.Add(1)
			go func(idx int, t *ResearchTask) {
				defer wg.Done()
				result, err := re.RunTask(ctx, t)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					t.Status = "failed"
					errs[idx] = fmt.Errorf("task %s: %w", t.ID, err)
				} else {
					t.Status = "completed"
					t.Result = result
					results[t.ID] = result
				}
			}(i, task)
		}
		wg.Wait()

		// Collect errors.
		for _, err := range errs {
			if err != nil {
				return results, err
			}
		}
	}

	return results, nil
}

// Synthesize combines task results using the plan's synthesis prompt.
// If no synthesizer is set, it concatenates results.
func Synthesize(plan *ResearchPlanV2, results map[string]string) string {
	if len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	if plan.Synthesizer != "" {
		sb.WriteString(plan.Synthesizer)
		sb.WriteString("\n\n")
	}
	sb.WriteString("## Research Results\n\n")
	for _, t := range plan.Tasks {
		if r, ok := results[t.ID]; ok {
			fmt.Fprintf(&sb, "### %s\n**Query:** %s\n\n%s\n\n", t.ID, t.Query, r)
		}
	}
	return sb.String()
}
