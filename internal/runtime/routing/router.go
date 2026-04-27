package routing

import (
	"context"
	"log/slog"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/tools"
)

// TaskType identifies the kind of inference task for routing decisions.
type TaskType string

const (
	TaskClassification TaskType = "classification"
	TaskEmbedding      TaskType = "embedding"
	TaskSummarization  TaskType = "summarization"
	TaskCommitMsg      TaskType = "commit_message"
)

// Candidate represents a model+provider option for routing.
type Candidate struct {
	Provider api.Provider
	Model    string
	IsLocal  bool
}

// ScoredCandidate is a candidate with its computed routing score.
type ScoredCandidate struct {
	Candidate
	Score float64

	// Factor breakdown for observability.
	CostScore         float64
	LatencyScore      float64
	QualityScore      float64
	ResourceScore     float64
	AvailabilityScore float64
}

// StatsProvider supplies observed performance data for routing decisions.
// Implementations may query OTEL data, QualityMonitor, or tool_usage tables.
type StatsProvider interface {
	// Stats returns observed performance stats for a model on a task type.
	// Returns zero-value CandidateStats if no data is available.
	Stats(ctx context.Context, model string, task TaskType) CandidateStats
}

// LoadProvider supplies local system load information.
type LoadProvider interface {
	// LoadAverage returns the current system load average (1-minute).
	LoadAverage() float64
}

// Router selects the optimal model/provider for each task type using
// multi-factor scoring informed by OTEL telemetry.
type Router struct {
	mu sync.RWMutex

	candidates map[TaskType][]Candidate
	weights    Weights
	budgets    map[TaskType]TaskBudget

	stats  StatsProvider
	load   LoadProvider
	logger *slog.Logger
}

// Option configures a Router.
type Option func(*Router)

// WithWeights sets custom factor weights.
func WithWeights(w Weights) Option {
	return func(r *Router) { r.weights = w }
}

// WithBudgets sets custom task budgets.
func WithBudgets(b map[TaskType]TaskBudget) Option {
	return func(r *Router) { r.budgets = b }
}

// WithStatsProvider sets the telemetry data source.
func WithStatsProvider(sp StatsProvider) Option {
	return func(r *Router) { r.stats = sp }
}

// WithLoadProvider sets the system load data source.
func WithLoadProvider(lp LoadProvider) Option {
	return func(r *Router) { r.load = lp }
}

// NewRouter creates a new inference router.
func NewRouter(opts ...Option) *Router {
	r := &Router{
		candidates: make(map[TaskType][]Candidate),
		weights:    DefaultWeights(),
		budgets:    DefaultTaskBudgets(),
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RegisterCandidate adds a model+provider as a candidate for a task type.
func (r *Router) RegisterCandidate(task TaskType, c Candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidates[task] = append(r.candidates[task], c)
}

// RegisterCandidateForAll adds a candidate for all task types.
func (r *Router) RegisterCandidateForAll(c Candidate) {
	for _, task := range []TaskType{TaskClassification, TaskEmbedding, TaskSummarization, TaskCommitMsg} {
		r.RegisterCandidate(task, c)
	}
}

// Route selects the best candidate for a task type.
// Returns the scored candidate with full factor breakdown.
// Returns nil if no candidates are registered for the task type.
func (r *Router) Route(ctx context.Context, task TaskType) *ScoredCandidate {
	r.mu.RLock()
	candidates := r.candidates[task]
	weights := r.weights
	budget := r.budgets[task]
	r.mu.RUnlock()

	if len(candidates) == 0 {
		return nil
	}

	// Get current system load (for local resource scoring).
	loadAvg := 0.0
	if r.load != nil {
		loadAvg = r.load.LoadAverage()
	}

	var best *ScoredCandidate
	for _, c := range candidates {
		// Get observed stats from telemetry.
		var stats CandidateStats
		if r.stats != nil {
			stats = r.stats.Stats(ctx, c.Model, task)
		}

		sc := &ScoredCandidate{
			Candidate:         c,
			CostScore:         ScoreCost(c.Model, c.IsLocal, budget),
			LatencyScore:      ScoreLatency(stats, c.IsLocal, budget),
			QualityScore:      ScoreQuality(c.Model, c.IsLocal, task),
			ResourceScore:     ScoreResource(c.IsLocal, loadAvg),
			AvailabilityScore: ScoreAvailability(stats),
		}
		sc.Score = CompositeScore(weights, sc.CostScore, sc.LatencyScore,
			sc.QualityScore, sc.ResourceScore, sc.AvailabilityScore)

		if best == nil || sc.Score > best.Score {
			best = sc
		}
	}

	if best != nil {
		r.logger.Info("routing decision",
			"task", task,
			"model", best.Model,
			"local", best.IsLocal,
			"score", best.Score,
			"cost", best.CostScore,
			"latency", best.LatencyScore,
			"quality", best.QualityScore,
			"resource", best.ResourceScore,
			"availability", best.AvailabilityScore,
		)
	}

	return best
}

// QualityMonitorStats adapts a tools.QualityMonitor as a StatsProvider.
type QualityMonitorStats struct {
	Monitor *tools.QualityMonitor
}

// Stats returns performance stats from the QualityMonitor.
// The model parameter is used as the tool name for lookup.
func (q *QualityMonitorStats) Stats(_ context.Context, model string, _ TaskType) CandidateStats {
	if q.Monitor == nil {
		return CandidateStats{}
	}
	rel := q.Monitor.Reliability(model)
	if rel.TotalCalls == 0 {
		return CandidateStats{}
	}
	return CandidateStats{
		ObservedP50Ms: rel.AvgDurationMs, // approximation: avg ≈ p50 for small samples
		SuccessRate:   rel.SuccessRate,
		SampleCount:   rel.TotalCalls,
	}
}

// StaticLoadProvider returns a fixed load average (useful for testing).
type StaticLoadProvider struct {
	Load float64
}

func (s StaticLoadProvider) LoadAverage() float64 { return s.Load }
