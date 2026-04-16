package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

const (
	// CacheWarmInterval is how often to ping the provider to keep the prompt cache alive.
	// Anthropic's cache TTL is 5 minutes; we ping at 4.5 minutes to stay ahead.
	CacheWarmInterval = 4*time.Minute + 30*time.Second

	// cacheWarmMaxTokens is the output limit for warming pings (we don't need output).
	cacheWarmMaxTokens = 1
)

// CacheWarmer keeps the provider's prompt cache alive by sending minimal
// requests at regular intervals. This prevents cache eviction during idle
// periods (e.g., user reading output or thinking between turns).
//
// Inspired by aider's --cache-keepalive-pings feature.
type CacheWarmer struct {
	provider Provider
	mu       sync.Mutex
	cancel   context.CancelFunc
	running  bool

	// Last request components to replay for cache warming.
	lastModel  string
	lastSystem string
	lastTools  []ToolDefinition

	// MaxPings limits the number of keep-alive pings (0 = unlimited).
	MaxPings int
	// PingCount tracks how many pings have been sent.
	PingCount int
}

// NewCacheWarmer creates a new cache warmer for the given provider.
func NewCacheWarmer(provider Provider) *CacheWarmer {
	return &CacheWarmer{provider: provider}
}

// UpdateContext updates the cached request components used for warming pings.
// Call this after each successful Turn() to keep the warmer in sync.
func (cw *CacheWarmer) UpdateContext(model, system string, tools []ToolDefinition) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.lastModel = model
	cw.lastSystem = system
	cw.lastTools = tools
}

// Start begins the background warming loop. If already running, this is a no-op.
func (cw *CacheWarmer) Start() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.running {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	cw.cancel = cancel
	cw.running = true

	go cw.loop(ctx)
}

// Stop halts the background warming loop.
func (cw *CacheWarmer) Stop() {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if cw.cancel != nil {
		cw.cancel()
		cw.cancel = nil
	}
	cw.running = false
}

// Restart stops and starts the warmer (e.g., after compaction changes context).
func (cw *CacheWarmer) Restart() {
	cw.Stop()
	cw.Start()
}

func (cw *CacheWarmer) loop(ctx context.Context) {
	ticker := time.NewTicker(CacheWarmInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cw.mu.Lock()
			if cw.MaxPings > 0 && cw.PingCount >= cw.MaxPings {
				cw.mu.Unlock()
				slog.Debug("cache warmer: max pings reached", "count", cw.PingCount)
				return
			}
			model := cw.lastModel
			system := cw.lastSystem
			tools := cw.lastTools
			cw.mu.Unlock()

			if model == "" || system == "" {
				continue // No context to warm yet.
			}

			cw.ping(ctx, model, system, tools)
		}
	}
}

func (cw *CacheWarmer) ping(ctx context.Context, model, system string, tools []ToolDefinition) {
	// Build a minimal request that touches the cached system prompt + tools
	// but generates no meaningful output.
	req := &Request{
		Model:     model,
		MaxTokens: cacheWarmMaxTokens,
		System:    system,
		Tools:     tools,
		Messages: []Message{
			{
				Role: RoleUser,
				Content: []ContentBlock{
					{Type: ContentTypeText, Text: "ping"},
				},
			},
		},
		Stream: true,
	}

	pingCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	events, errc := cw.provider.Send(pingCtx, req)

	// Drain the response (we don't need it).
	for range events {
		// Discard.
	}

	err := <-errc
	cw.mu.Lock()
	cw.PingCount++
	cw.mu.Unlock()

	if err != nil {
		slog.Debug("cache warmer: ping failed", "error", err)
	} else {
		slog.Debug("cache warmer: ping successful", "model", model)
	}
}

// MarshalJSON implements custom marshaling (for debugging).
func (cw *CacheWarmer) MarshalJSON() ([]byte, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return json.Marshal(struct {
		Running   bool `json:"running"`
		PingCount int  `json:"ping_count"`
		MaxPings  int  `json:"max_pings"`
	}{
		Running:   cw.running,
		PingCount: cw.PingCount,
		MaxPings:  cw.MaxPings,
	})
}
