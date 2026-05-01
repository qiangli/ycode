// Package tools — smartrouter provides intelligent tool selection using
// vector embeddings for semantic matching and tool usage history for
// preference learning. It replaces keyword-only pre-activation with
// a multi-signal scoring pipeline.
package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/qiangli/ycode/internal/storage"
)

const (
	// toolEmbeddingCollection is the vector store collection for tool embeddings.
	toolEmbeddingCollection = "tool_embeddings"

	// maxSemanticActivations is the cap on tools activated by semantic matching.
	maxSemanticActivations = 5

	// semanticScoreThreshold is the minimum cosine similarity for activation (0-1).
	semanticScoreThreshold = 0.3
)

// ToolUsageStats holds aggregated usage data for a single tool.
type ToolUsageStats struct {
	Name        string
	CallCount   int
	SuccessRate float64
	AvgDuration float64 // milliseconds
	LastUsed    time.Time
	IsPreferred bool // user explicitly prefers this tool (from memory/feedback)
}

// UsageStatsProvider returns tool usage statistics.
// Implementations can query SQLite tool_usage table or QualityMonitor.
type UsageStatsProvider func(ctx context.Context) ([]ToolUsageStats, error)

// SmartRouter selects tools using semantic similarity and usage history.
type SmartRouter struct {
	vectorStore   storage.VectorStore
	statsProvider UsageStatsProvider
	indexed       bool
}

// NewSmartRouter creates a smart tool router.
// vectorStore may be nil (disables semantic matching, falls back to keyword scoring).
// statsProvider may be nil (disables preference boosting).
func NewSmartRouter(vectorStore storage.VectorStore, statsProvider UsageStatsProvider) *SmartRouter {
	return &SmartRouter{
		vectorStore:   vectorStore,
		statsProvider: statsProvider,
	}
}

// IndexTools indexes all tool descriptions into the vector store for semantic search.
// Call once at startup after all tools are registered.
func (sr *SmartRouter) IndexTools(ctx context.Context, registry *Registry) error {
	if sr.vectorStore == nil {
		return nil
	}

	var docs []storage.VectorDocument
	for _, spec := range registry.All() {
		content := spec.Name + ": " + spec.Description
		docs = append(docs, storage.VectorDocument{
			Document: storage.Document{
				ID:      spec.Name,
				Content: content,
				Metadata: map[string]string{
					"name":     spec.Name,
					"category": fmt.Sprintf("%d", spec.Category),
				},
			},
		})
	}

	if len(docs) == 0 {
		return nil
	}

	if err := sr.vectorStore.AddDocuments(ctx, toolEmbeddingCollection, docs); err != nil {
		return fmt.Errorf("index tool embeddings: %w", err)
	}
	sr.indexed = true
	return nil
}

// routedTool is a tool with a composite relevance score.
type routedTool struct {
	Name            string
	SemanticScore   float64 // 0-1, from vector similarity
	PreferenceBoost float64 // 0-1, from usage history
	CompositeScore  float64 // weighted combination
}

// SelectTools returns the best tools for a user query, combining semantic
// similarity with user preference signals.
func (sr *SmartRouter) SelectTools(ctx context.Context, registry *Registry, query string, maxResults int) []string {
	if maxResults <= 0 {
		maxResults = maxSemanticActivations
	}

	candidates := make(map[string]*routedTool)

	// Signal 1: Semantic similarity via vector embeddings.
	if sr.vectorStore != nil && sr.indexed {
		sr.addSemanticScores(ctx, candidates, query)
	}

	// Signal 2: User preference from usage history.
	if sr.statsProvider != nil {
		sr.addPreferenceScores(ctx, candidates, registry)
	}

	// Compute composite scores and rank.
	var ranked []routedTool
	for _, rt := range candidates {
		// Weighted combination: 70% semantic, 30% preference.
		rt.CompositeScore = 0.7*rt.SemanticScore + 0.3*rt.PreferenceBoost
		if rt.CompositeScore > 0 {
			ranked = append(ranked, *rt)
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].CompositeScore > ranked[j].CompositeScore
	})

	// Filter out always-available tools (they don't need activation).
	var result []string
	for _, rt := range ranked {
		if len(result) >= maxResults {
			break
		}
		spec, ok := registry.Get(rt.Name)
		if !ok || spec.AlwaysAvailable {
			continue
		}
		result = append(result, rt.Name)
	}
	return result
}

// addSemanticScores queries the vector store for similar tools.
func (sr *SmartRouter) addSemanticScores(ctx context.Context, candidates map[string]*routedTool, query string) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	results, err := sr.vectorStore.QueryByText(queryCtx, toolEmbeddingCollection, query, maxSemanticActivations*2)
	if err != nil {
		return
	}

	for _, r := range results {
		name := r.Document.Metadata["name"]
		if name == "" {
			continue
		}
		score := r.Score
		if score < semanticScoreThreshold {
			continue
		}

		rt, ok := candidates[name]
		if !ok {
			rt = &routedTool{Name: name}
			candidates[name] = rt
		}
		rt.SemanticScore = score
	}
}

// addPreferenceScores boosts tools the user has used frequently and successfully.
func (sr *SmartRouter) addPreferenceScores(ctx context.Context, candidates map[string]*routedTool, registry *Registry) {
	stats, err := sr.statsProvider(ctx)
	if err != nil || len(stats) == 0 {
		return
	}

	// Find max call count for normalization.
	maxCalls := 1
	for _, s := range stats {
		if s.CallCount > maxCalls {
			maxCalls = s.CallCount
		}
	}

	for _, s := range stats {
		// Skip tools with low usage or poor success rate.
		if s.CallCount < 3 || s.SuccessRate < 0.5 {
			continue
		}

		// Preference score = normalized frequency * success rate.
		freqScore := float64(s.CallCount) / float64(maxCalls)
		prefScore := freqScore * s.SuccessRate

		// Cap at 1.0.
		if prefScore > 1.0 {
			prefScore = 1.0
		}

		// Explicit preference gets a full boost.
		if s.IsPreferred {
			prefScore = 1.0
		}

		rt, ok := candidates[s.Name]
		if !ok {
			// Only boost tools that are already candidates from semantic search
			// or are in the registry. Don't introduce random popular tools.
			if _, exists := registry.Get(s.Name); !exists {
				continue
			}
			rt = &routedTool{Name: s.Name}
			candidates[s.Name] = rt
		}
		rt.PreferenceBoost = prefScore
	}
}

// FormatToolSuggestion formats a redirect suggestion for the LLM.
func FormatToolSuggestion(currentTool string, alternatives []string, reason string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Instead of `%s`, consider using a more specialized tool:\n", currentTool)
	for _, alt := range alternatives {
		fmt.Fprintf(&b, "- `%s`\n", alt)
	}
	if reason != "" {
		fmt.Fprintf(&b, "\nReason: %s\n", reason)
	}
	b.WriteString("\nCall ToolSearch to activate these tools if they are not yet available.")
	return b.String()
}
