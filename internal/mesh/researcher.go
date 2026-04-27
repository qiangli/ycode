package mesh

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiangli/ycode/internal/bus"
)

// Researcher performs background web research for unknown errors.
type Researcher struct {
	b      bus.Bus
	logger *slog.Logger
	limit  int // max searches per 10 minutes

	// SearchFunc performs a web search and returns results.
	SearchFunc func(ctx context.Context, query string) (string, error)

	// SaveFunc saves research results as reference memory.
	SaveFunc func(ctx context.Context, name, content string) error

	cancel  context.CancelFunc
	healthy atomic.Bool

	mu          sync.Mutex
	searchCount int
	windowStart time.Time
}

// NewResearcher creates a researcher agent.
func NewResearcher(b bus.Bus, limit int) *Researcher {
	if limit <= 0 {
		limit = 3
	}
	return &Researcher{
		b:           b,
		logger:      slog.Default(),
		limit:       limit,
		windowStart: time.Now(),
	}
}

func (r *Researcher) Name() string  { return "researcher" }
func (r *Researcher) Healthy() bool { return r.healthy.Load() }

func (r *Researcher) Start(ctx context.Context) error {
	ctx, r.cancel = context.WithCancel(ctx)
	r.healthy.Store(true)
	go r.listen(ctx)
	return nil
}

func (r *Researcher) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.healthy.Store(false)
}

func (r *Researcher) listen(ctx context.Context) {
	ch, unsub := r.b.Subscribe(bus.EventDiagReport, bus.EventFixFailed)
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-ch:
			r.handleEvent(ctx, ev)
		}
	}
}

func (r *Researcher) handleEvent(ctx context.Context, ev bus.Event) {
	if !r.canSearch() {
		r.logger.Debug("mesh.researcher.rate_limited")
		return
	}

	if r.SearchFunc == nil || r.SaveFunc == nil {
		return
	}

	// Extract error info from event data for search query.
	query := string(ev.Data)
	if len(query) > 200 {
		query = query[:200]
	}

	r.logger.Info("mesh.researcher.searching", "query_length", len(query))

	result, err := r.SearchFunc(ctx, query)
	if err != nil {
		r.logger.Debug("mesh.researcher.search_error", "error", err)
		return
	}

	// Save as reference memory.
	name := "research-" + time.Now().Format("20060102-150405")
	if err := r.SaveFunc(ctx, name, result); err != nil {
		r.logger.Debug("mesh.researcher.save_error", "error", err)
		return
	}

	r.b.Publish(bus.Event{Type: bus.EventResearchDone, Data: []byte(result)})
	r.logger.Info("mesh.researcher.saved", "name", name)
}

func (r *Researcher) canSearch() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if time.Since(r.windowStart) > 10*time.Minute {
		r.searchCount = 0
		r.windowStart = time.Now()
	}

	if r.searchCount >= r.limit {
		return false
	}
	r.searchCount++
	return true
}
