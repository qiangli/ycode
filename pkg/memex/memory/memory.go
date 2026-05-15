package memory

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Stamper populates provenance fields on a Memory before it is persisted.
// Implementations live outside this package (typically in RuntimeContext)
// and have access to the current persona, project, session, and host.
// Set on Manager via SetStamper; when nil, no stamping occurs and Origin
// is left untouched.
type Stamper interface {
	Stamp(mem *Memory)
}

// Manager coordinates memory operations across global, user, team, and
// project scopes.
type Manager struct {
	projectStore *Store
	globalStore  *Store // may be nil if no global dir configured
	userStore    *Store // may be nil if no user dir configured
	teamStore    *Store // may be nil if no team dir configured
	index        *Index // project-level index

	bleveSearcher  *BleveSearcher  // optional Bleve full-text search
	vectorSearcher *VectorSearcher // optional vector semantic search
	entityIndex    *EntityIndex    // optional entity extraction and linking
	provider       MemoryProvider  // optional pluggable provider (supplements stores)
	stamper        Stamper         // optional provenance stamper invoked on Save
	timeBucket     *TimeBucket     // day-grained index over CreatedAt/LastAccessedAt
	timeBucketInit sync.Once       // lazy first-use rebuild of the bucket
}

// NewManager creates a new memory manager with a project-scoped store.
func NewManager(dir string) (*Manager, error) {
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		projectStore: store,
		index:        NewIndex(dir),
		timeBucket:   NewTimeBucket(),
	}, nil
}

// NewManagerWithGlobal creates a manager with both global and project stores.
// Global memories are shared across all projects; project memories are scoped.
func NewManagerWithGlobal(globalDir, projectDir string) (*Manager, error) {
	projectStore, err := NewStore(projectDir)
	if err != nil {
		return nil, fmt.Errorf("project store: %w", err)
	}

	var globalStore *Store
	if globalDir != "" {
		globalStore, err = NewStore(globalDir)
		if err != nil {
			return nil, fmt.Errorf("global store: %w", err)
		}
	}

	return &Manager{
		projectStore: projectStore,
		globalStore:  globalStore,
		index:        NewIndex(projectDir),
		timeBucket:   NewTimeBucket(),
	}, nil
}

// SetBleveSearcher attaches a Bleve-backed searcher for full-text memory search.
func (m *Manager) SetBleveSearcher(b *BleveSearcher) {
	m.bleveSearcher = b
}

// SetVectorSearcher attaches a vector-based searcher for semantic memory search.
func (m *Manager) SetVectorSearcher(v *VectorSearcher) {
	m.vectorSearcher = v
}

// SetProvider attaches an optional MemoryProvider that supplements the
// default Store-based flow. When set, lifecycle hooks (e.g., OnMemoryWrite)
// are called after the primary store operations succeed.
func (m *Manager) SetProvider(p MemoryProvider) {
	m.provider = p
}

// SetStamper attaches a provenance stamper invoked on every Save before
// persistence. Used to populate Memory.Origin from the surrounding runtime
// (persona, project, session, host). Nil is allowed (default: no stamping).
func (m *Manager) SetStamper(s Stamper) {
	m.stamper = s
}

// SetUserStore attaches a user-scope store, typically rooted at
// ~/.agents/ycode/users/<personaID>/memory/. Nil-safe: when unset,
// ScopeUser memories fall back to the project store.
func (m *Manager) SetUserStore(s *Store) {
	m.userStore = s
}

// SetTeamStore attaches a team-scope store, typically rooted at
// ~/.agents/ycode/teams/<teamID>/memory/. Nil-safe: when unset,
// ScopeTeam memories fall back to the project store.
func (m *Manager) SetTeamStore(s *Store) {
	m.teamStore = s
}

// UserStore returns the user-scope store (may be nil).
func (m *Manager) UserStore() *Store { return m.userStore }

// TeamStore returns the team-scope store (may be nil).
func (m *Manager) TeamStore() *Store { return m.teamStore }

// Save persists a memory to the appropriate store based on scope.
func (m *Manager) Save(mem *Memory) error {
	if m.stamper != nil {
		m.stamper.Stamp(mem)
	}

	ctx := context.Background()
	tracer := otel.Tracer("ycode.memory")
	_, span := tracer.Start(ctx, "ycode.memory.save",
		trace.WithAttributes(
			attribute.String("memory.name", mem.Name),
			attribute.String("memory.type", string(mem.Type)),
			attribute.String("memory.scope", string(mem.EffectiveScope())),
			attribute.Float64("memory.importance", mem.Importance),
		))
	defer span.End()

	store := m.storeForScope(mem.EffectiveScope())
	if err := store.Save(mem); err != nil {
		return fmt.Errorf("save memory: %w", err)
	}

	filename := filepath.Base(mem.FilePath)
	if err := m.index.AddEntry(mem.Name, filename, mem.Description); err != nil {
		return fmt.Errorf("update index: %w", err)
	}

	// Index in Bleve for full-text search.
	if m.bleveSearcher != nil {
		m.bleveSearcher.IndexMemory(mem)
	}

	// Extract and link entities (Phase 4).
	if m.entityIndex != nil {
		entities := ExtractEntities(mem.Content)
		mem.Entities = make([]string, 0, len(entities))
		for _, e := range entities {
			m.entityIndex.Link(e, mem.Name)
			mem.Entities = append(mem.Entities, e.Name)
		}
	}

	// Notify the pluggable provider if one is attached.
	if m.provider != nil {
		if err := m.provider.OnMemoryWrite(context.Background(), mem); err != nil {
			slog.Warn("memory provider OnMemoryWrite failed", "error", err)
		}
	}

	// Update the time-bucket index so date-shaped queries find this
	// memory without a full rebuild.
	if m.timeBucket != nil {
		m.timeBucket.Add(mem)
	}

	return nil
}

// Recall retrieves memories matching a query from all scopes.
// Uses Reciprocal Rank Fusion (RRF) across all available search backends,
// applies composite scoring (recency + importance), then MMR diversity re-ranking.
func (m *Manager) Recall(query string, maxResults int) ([]SearchResult, error) {
	ctx := context.Background()
	tracer := otel.Tracer("ycode.memory")
	_, span := tracer.Start(ctx, "ycode.memory.recall",
		trace.WithAttributes(
			attribute.String("memory.query", query),
			attribute.Int("memory.max_results", maxResults),
		))
	defer span.End()

	memories, err := m.All()
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	// Build a name→memory lookup for enriching search results.
	memByName := make(map[string]*Memory, len(memories))
	for _, mem := range memories {
		memByName[mem.Name] = mem
	}

	// Collect results from all available backends in parallel.
	resultSets := make(map[string][]SearchResult)

	if m.vectorSearcher != nil {
		vectorResults := m.vectorSearcher.Search(query, maxResults*2)
		for i := range vectorResults {
			if full, ok := memByName[vectorResults[i].Memory.Name]; ok {
				vectorResults[i].Memory = full
			}
			vectorResults[i].Source = "vector"
		}
		if len(vectorResults) > 0 {
			resultSets["vector"] = vectorResults
		}
	}

	if m.bleveSearcher != nil {
		bleveResults := m.bleveSearcher.Search(query, maxResults*2)
		for i := range bleveResults {
			if full, ok := memByName[bleveResults[i].Memory.Name]; ok {
				bleveResults[i].Memory = full
			}
			bleveResults[i].Source = "bleve"
		}
		if len(bleveResults) > 0 {
			resultSets["bleve"] = bleveResults
		}
	}

	// Entity-based retrieval (Phase 4 integration point).
	if m.entityIndex != nil {
		entityResults := m.entityIndex.SearchMemories(query, memByName, maxResults*2)
		if len(entityResults) > 0 {
			resultSets["entity"] = entityResults
		}
	}

	// Always run keyword search as a signal (not just fallback).
	keywordResults := Search(memories, query, maxResults*2)
	for i := range keywordResults {
		keywordResults[i].Source = "keyword"
	}
	if len(keywordResults) > 0 {
		resultSets["keyword"] = keywordResults
	}

	// Time-window fast path: if the query has temporal intent, build an
	// in-window membership set so we can both contribute a result set
	// to RRF (gives the bucket its own ranking dimension) AND apply a
	// post-fusion boost to in-window memories. The boost matters because
	// RRF normalizes by rank — without it, in-window hits get diluted
	// when other backends also rank the out-of-window memory.
	var timeWindowSet map[string]struct{}
	if window := DetectTimeWindow(query); window != nil {
		m.ensureTimeBucket(memories)
		names := m.timeBucket.Range(window.Start, window.End)
		slog.Debug("memory.recall.time_window",
			"label", window.Label,
			"start", window.Start,
			"end", window.End,
			"bucket_hits", len(names),
		)
		if len(names) > 0 {
			timeWindowSet = make(map[string]struct{}, len(names))
			timeResults := make([]SearchResult, 0, len(names))
			for _, name := range names {
				mem, ok := memByName[name]
				if !ok {
					continue
				}
				timeWindowSet[name] = struct{}{}
				timeResults = append(timeResults, SearchResult{
					Memory: mem,
					Score:  1.0,
					Source: "time",
				})
			}
			if len(timeResults) > 0 {
				resultSets["time"] = timeResults
			}
		}
	}

	// Fuse results with RRF.
	fusionWeights := DefaultFusionWeights()
	results := ReciprocalRankFusion(resultSets, fusionWeights.RRFk)

	// Apply composite scoring on top of fused scores.
	weights := DefaultWeights()
	for i := range results {
		results[i].Score = CompositeScore(
			results[i].Score,                   // fused RRF score
			results[i].Memory.UpdatedAt,        // recency
			results[i].Memory.EffectiveValue(), // dynamic value (Phase 2)
			weights,
		)
		// Scope cascade weights — project boosted, other scopes attenuated
		// so cross-repo memories surface only when intrinsically relevant.
		switch results[i].Memory.EffectiveScope() {
		case ScopeProject:
			results[i].Score *= 1.1
		case ScopeUser, ScopeTeam:
			results[i].Score *= 0.9
		case ScopeGlobal:
			results[i].Score *= 0.85
		}
		// Time-window boost: when the query has temporal intent, lift
		// in-window memories above same-relevance out-of-window ones.
		// Strong multiplier because the user's question is literally
		// "what is in this window" — out-of-window matches are noise.
		if timeWindowSet != nil {
			if _, in := timeWindowSet[results[i].Memory.Name]; in {
				results[i].Score *= 3.0
				if results[i].Source != "time" {
					results[i].Source = "time"
				}
			}
		}
		// Memories past their validity window get a penalty (Phase 6).
		if results[i].Memory.ValidUntil != nil && time.Now().After(*results[i].Memory.ValidUntil) {
			results[i].Score *= 0.3
		}
	}

	// Re-sort after composite scoring.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// MMR diversity re-ranking.
	results = MMRRerank(results, fusionWeights.MMRLambda, maxResults)

	span.SetAttributes(
		attribute.Int("memory.results_count", len(results)),
	)
	slog.Info("memory.recall",
		"query", query,
		"max_results", maxResults,
		"results_count", len(results),
	)

	return results, nil
}

// Forget removes a memory by name from all scopes.
func (m *Manager) Forget(name string) error {
	memories, err := m.All()
	if err != nil {
		return err
	}

	for _, mem := range memories {
		if mem.Name == name {
			store := m.storeForScope(mem.EffectiveScope())
			filename := filepath.Base(mem.FilePath)
			if err := store.Delete(mem.FilePath); err != nil {
				return fmt.Errorf("delete memory: %w", err)
			}
			if m.bleveSearcher != nil {
				m.bleveSearcher.RemoveMemory(name)
			}
			if m.entityIndex != nil {
				m.entityIndex.Unlink(name)
			}
			if m.timeBucket != nil {
				m.timeBucket.Remove(name)
			}
			return m.index.RemoveEntry(filename)
		}
	}

	return fmt.Errorf("memory %q not found", name)
}

// All returns all stored memories from all scopes (project + global +
// user + team, when configured).
func (m *Manager) All() ([]*Memory, error) {
	projectMems, err := m.projectStore.List()
	if err != nil {
		return nil, fmt.Errorf("list project memories: %w", err)
	}
	for _, mem := range projectMems {
		if mem.Scope == "" {
			mem.Scope = ScopeProject
		}
	}

	out := projectMems

	if m.globalStore != nil {
		globalMems, err := m.globalStore.List()
		if err != nil {
			return nil, fmt.Errorf("list global memories: %w", err)
		}
		for _, mem := range globalMems {
			mem.Scope = ScopeGlobal
		}
		out = append(globalMems, out...)
	}

	if m.userStore != nil {
		userMems, err := m.userStore.List()
		if err != nil {
			return nil, fmt.Errorf("list user memories: %w", err)
		}
		for _, mem := range userMems {
			mem.Scope = ScopeUser
		}
		out = append(out, userMems...)
	}

	if m.teamStore != nil {
		teamMems, err := m.teamStore.List()
		if err != nil {
			return nil, fmt.Errorf("list team memories: %w", err)
		}
		for _, mem := range teamMems {
			mem.Scope = ScopeTeam
		}
		out = append(out, teamMems...)
	}

	return out, nil
}

// ReadIndex returns the MEMORY.md contents.
func (m *Manager) ReadIndex() (string, error) {
	return m.index.Read()
}

// ensureTimeBucket performs a one-shot rebuild of the time-bucket index
// from the given memory list. Subsequent calls are no-ops because
// Save/Forget maintain the bucket incrementally. The sync.Once guards
// against duplicate work when Recall is invoked concurrently.
func (m *Manager) ensureTimeBucket(memories []*Memory) {
	if m.timeBucket == nil {
		return
	}
	m.timeBucketInit.Do(func() {
		m.timeBucket.Rebuild(memories)
	})
}

// RebuildTimeBucket forces a fresh build of the time-bucket index from
// all current memories. Intended for use by the Dreamer at consolidation
// time to repair any drift from incremental updates.
func (m *Manager) RebuildTimeBucket() error {
	if m.timeBucket == nil {
		return nil
	}
	memories, err := m.All()
	if err != nil {
		return fmt.Errorf("rebuild time bucket: %w", err)
	}
	m.timeBucket.Rebuild(memories)
	return nil
}

// FormatProvenance returns a short suffix like "[macbook · ycode]" when
// the memory was captured on a different host or in a different project
// than the current execution context. Returns the empty string when there
// is no Origin or when host and project both match (no useful provenance
// to surface).
//
// currentHost and currentProjectID should be the values from the caller's
// runtime (typically os.Hostname() and the resolved origin.ProjectID). If
// either is empty, the comparison is skipped for that dimension.
func FormatProvenance(mem *Memory, currentHost, currentProjectID string) string {
	if mem == nil || mem.Origin == nil {
		return ""
	}
	var parts []string
	if mem.Origin.Host != "" && currentHost != "" && mem.Origin.Host != currentHost {
		parts = append(parts, mem.Origin.Host)
	}
	if mem.Origin.ProjectID != "" && currentProjectID != "" && mem.Origin.ProjectID != currentProjectID {
		parts = append(parts, mem.Origin.ProjectID)
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " · ") + "]"
}

// Store returns the project store (for backward compatibility).
func (m *Manager) Store() *Store {
	return m.projectStore
}

// GlobalStore returns the global store (may be nil).
func (m *Manager) GlobalStore() *Store {
	return m.globalStore
}

// storeForScope returns the appropriate store for a given scope. Falls
// back to the project store when the requested scope has no backing dir
// configured (keeps memex usable in minimal setups).
func (m *Manager) storeForScope(scope Scope) *Store {
	switch scope {
	case ScopeGlobal:
		if m.globalStore != nil {
			return m.globalStore
		}
	case ScopeUser:
		if m.userStore != nil {
			return m.userStore
		}
	case ScopeTeam:
		if m.teamStore != nil {
			return m.teamStore
		}
	}
	return m.projectStore
}
