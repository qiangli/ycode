//go:build eval_e2e

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/eval"
)

func TestE2E(t *testing.T) {
	provider, model, err := eval.ProviderFromEnv()
	if err != nil {
		t.Skipf("skipping E2E evals: %v", err)
	}

	cfg := eval.RunConfig{
		Provider: os.Getenv("EVAL_PROVIDER"),
		Model:    model,
		Version:  "test",
	}

	runner := eval.AgentRunner(cfg, provider)

	for _, scenario := range Scenarios() {
		scenario := scenario
		t.Run(scenario.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), scenario.EffectiveTimeout()*time.Duration(scenario.EffectiveTrials()))
			defer cancel()

			result, runErr := runner.Run(ctx, scenario)
			if runErr != nil {
				t.Fatalf("runner error: %v", runErr)
			}

			passed := 0
			for _, trial := range result.Trials {
				if trial.Passed {
					passed++
				} else {
					t.Logf("trial %d failed: %s", trial.Trial, trial.Error)
				}
			}

			threshold := scenario.EffectivePassThreshold()
			if passed < threshold {
				t.Errorf("%s: %d/%d trials passed (need %d), pass@k=%.2f",
					scenario.Name, passed, len(result.Trials), threshold, result.Metrics.PassAtK)
			} else {
				t.Logf("%s: %d/%d passed, pass@k=%.2f, pass^k=%.2f",
					scenario.Name, passed, len(result.Trials),
					result.Metrics.PassAtK, result.Metrics.PassPowK)
			}
		})
	}
}
