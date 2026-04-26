package eval

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ScheduleConfig configures recurring eval runs.
type ScheduleConfig struct {
	Interval  time.Duration // How often to run (e.g. 24h)
	Tiers     []Tier        // Which tiers to run
	ReportDir string        // Where to store reports
	Provider  string        // Provider name
	Model     string        // Model name
	Version   string        // Version/SHA to tag reports with
}

// ScheduleRunner manages recurring eval runs using a simple ticker.
// For production use, integrate with team.CronRegistry.
type ScheduleRunner struct {
	cfg       ScheduleConfig
	runner    *Runner
	store     *ReportStore
	logger    *slog.Logger
	scenarios map[Tier][]*Scenario
	cancel    context.CancelFunc
}

// NewScheduleRunner creates a new scheduled eval runner.
func NewScheduleRunner(cfg ScheduleConfig, runner *Runner, store *ReportStore, logger *slog.Logger) *ScheduleRunner {
	return &ScheduleRunner{
		cfg:       cfg,
		runner:    runner,
		store:     store,
		logger:    logger,
		scenarios: make(map[Tier][]*Scenario),
	}
}

// AddScenarios registers scenarios for a given tier.
func (sr *ScheduleRunner) AddScenarios(tier Tier, scenarios []*Scenario) {
	sr.scenarios[tier] = append(sr.scenarios[tier], scenarios...)
}

// Start begins the recurring eval loop. Blocks until context is cancelled.
func (sr *ScheduleRunner) Start(ctx context.Context) error {
	ctx, sr.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(sr.cfg.Interval)
	defer ticker.Stop()

	// Run immediately on start.
	if err := sr.runOnce(ctx); err != nil {
		sr.logger.Warn("eval run failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := sr.runOnce(ctx); err != nil {
				sr.logger.Warn("eval run failed", "error", err)
			}
		}
	}
}

// Stop cancels the running schedule.
func (sr *ScheduleRunner) Stop() {
	if sr.cancel != nil {
		sr.cancel()
	}
}

// runOnce executes all registered scenarios once and saves the report.
func (sr *ScheduleRunner) runOnce(ctx context.Context) error {
	start := time.Now()
	sr.logger.Info("starting eval run", "tiers", len(sr.cfg.Tiers))

	var allResults []ScenarioResult
	var failures int

	for _, tier := range sr.cfg.Tiers {
		scenarios, ok := sr.scenarios[tier]
		if !ok {
			continue
		}

		for _, s := range scenarios {
			sr.logger.Info("running scenario", "name", s.Name, "tier", tier)

			result, err := sr.runner.Run(ctx, s)
			if err != nil {
				sr.logger.Error("scenario error", "name", s.Name, "error", err)
				failures++
				continue
			}

			allResults = append(allResults, *result)

			// Check pass threshold.
			passed := 0
			for _, t := range result.Trials {
				if t.Passed {
					passed++
				}
			}
			if passed < s.EffectivePassThreshold() {
				failures++
				sr.logger.Warn("scenario below threshold",
					"name", s.Name,
					"passed", passed,
					"threshold", s.EffectivePassThreshold(),
					"pass_at_k", result.Metrics.PassAtK)
			}
		}
	}

	// Compute aggregate composite score.
	var totalPassAtK, totalPassPowK, totalFlakiness float64
	for _, r := range allResults {
		totalPassAtK += r.Metrics.PassAtK
		totalPassPowK += r.Metrics.PassPowK
		totalFlakiness += r.Metrics.Flakiness
	}
	n := float64(len(allResults))
	if n == 0 {
		n = 1
	}

	composite := CompositeScore(
		totalPassAtK/n,
		totalPassPowK/n,
		totalFlakiness/n,
		1.0, // tool accuracy placeholder
		1.0, // cost efficiency placeholder
	)

	report := &Report{
		ID:        fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), sr.cfg.Version),
		Version:   sr.cfg.Version,
		Provider:  sr.cfg.Provider,
		Model:     sr.cfg.Model,
		Tier:      "all",
		Timestamp: time.Now(),
		Scenarios: allResults,
		Composite: composite,
		Duration:  time.Since(start),
	}

	if err := sr.store.Save(report); err != nil {
		return fmt.Errorf("save report: %w", err)
	}

	sr.logger.Info("eval run complete",
		"duration", time.Since(start),
		"scenarios", len(allResults),
		"failures", failures,
		"composite", fmt.Sprintf("%.0f/100", composite*100))

	return nil
}
