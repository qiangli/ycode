package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/runtime/session"
)

// StopHooksConfig configures the post-turn stop hooks pipeline.
type StopHooksConfig struct {
	// MemoryExtractionEnabled enables post-turn memory extraction.
	MemoryExtractionEnabled bool

	// MemoryExtractionInterval is the minimum turns between extractions.
	MemoryExtractionInterval int

	// DreamEnabled enables auto-dream consolidation.
	DreamEnabled bool

	// DreamMinHours is the minimum hours between dream sessions.
	DreamMinHours float64

	// DreamMinSessions is the minimum sessions before dreaming.
	DreamMinSessions int

	// CacheParamsSharingEnabled enables cache-safe parameter snapshots.
	CacheParamsSharingEnabled bool
}

// DefaultStopHooksConfig returns sensible defaults.
func DefaultStopHooksConfig() StopHooksConfig {
	return StopHooksConfig{
		MemoryExtractionEnabled:   true,
		MemoryExtractionInterval:  5,
		DreamEnabled:              true,
		DreamMinHours:             24.0,
		DreamMinSessions:          5,
		CacheParamsSharingEnabled: true,
	}
}

// StopHooksState tracks state across turns for the stop hooks pipeline.
type StopHooksState struct {
	mu                 sync.Mutex
	turnCount          int
	lastExtractionAt   int // turn number of last extraction
	lastDreamAt        time.Time
	sessionsSinceDream int
	cacheParams        *CacheSafeParams
}

// NewStopHooksState creates a new stop hooks state tracker.
func NewStopHooksState() *StopHooksState {
	return &StopHooksState{}
}

// CacheSafeParams is a snapshot of prompt parameters that can be shared
// with forked agents to reuse the parent's prompt cache.
//
// Inspired by Claude Code's CacheSafeParams.
type CacheSafeParams struct {
	SystemPromptHash string
	ToolsHash        string
	Model            string
	UserContextHash  string
	SnapshotAt       time.Time
}

// ComputeCacheSafeParams creates a parameter snapshot from current state.
func ComputeCacheSafeParams(systemPrompt string, toolsJSON string, model string, userContext string) *CacheSafeParams {
	return &CacheSafeParams{
		SystemPromptHash: hashString(systemPrompt),
		ToolsHash:        hashString(toolsJSON),
		Model:            model,
		UserContextHash:  hashString(userContext),
		SnapshotAt:       time.Now(),
	}
}

func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8]) // 16-char hex
}

// RunStopHooks executes the post-turn pipeline. All actions are async
// and non-blocking. This orchestrates memory extraction, auto-dream,
// and cache parameter snapshots.
//
// Inspired by Claude Code's handleStopHooks() in query/stopHooks.ts.
func RunStopHooks(ctx context.Context, state *StopHooksState, cfg StopHooksConfig,
	messages []session.ConversationMessage,
	extractFn func(ctx context.Context, messages []session.ConversationMessage),
	dreamFn func(ctx context.Context),
) {
	state.mu.Lock()
	state.turnCount++
	currentTurn := state.turnCount
	state.mu.Unlock()

	// 1. Memory extraction (async, gated by interval).
	if cfg.MemoryExtractionEnabled && extractFn != nil {
		state.mu.Lock()
		shouldExtract := currentTurn-state.lastExtractionAt >= cfg.MemoryExtractionInterval
		if shouldExtract {
			state.lastExtractionAt = currentTurn
		}
		state.mu.Unlock()

		if shouldExtract {
			go func() {
				extractCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				defer cancel()
				extractFn(extractCtx, messages)
				slog.Debug("stop_hooks: memory extraction completed", "turn", currentTurn)
			}()
		}
	}

	// 2. Auto-dream (async, gated by time and session count).
	if cfg.DreamEnabled && dreamFn != nil {
		state.mu.Lock()
		state.sessionsSinceDream++
		hoursSinceDream := time.Since(state.lastDreamAt).Hours()
		shouldDream := hoursSinceDream >= cfg.DreamMinHours &&
			state.sessionsSinceDream >= cfg.DreamMinSessions
		if shouldDream {
			state.lastDreamAt = time.Now()
			state.sessionsSinceDream = 0
		}
		state.mu.Unlock()

		if shouldDream {
			go func() {
				dreamCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
				defer cancel()
				dreamFn(dreamCtx)
				slog.Debug("stop_hooks: auto-dream completed", "turn", currentTurn)
			}()
		}
	}

	slog.Debug("stop_hooks: pipeline completed", "turn", currentTurn)
}

// GetCacheParams returns the latest cache-safe parameters snapshot.
func (s *StopHooksState) GetCacheParams() *CacheSafeParams {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cacheParams
}

// UpdateCacheParams stores a new cache-safe parameters snapshot.
func (s *StopHooksState) UpdateCacheParams(params *CacheSafeParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheParams = params
}
