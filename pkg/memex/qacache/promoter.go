package qacache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// PromotionSaver persists a single promoted Q→A entry as a memory. The
// implementation lives in the runtime — typically a thin closure over
// memex Manager.Save — so this package stays free of the memory import
// (avoids a cycle since qacache may eventually be referenced by memory
// for invalidation hooks).
type PromotionSaver func(ctx context.Context, e *Entry) error

// Promoter walks the cache's promotion candidates and persists each one
// via the supplied saver, then drops the candidate from the cache. One
// Promoter per Cache instance.
type Promoter struct {
	cache *Cache
	save  PromotionSaver
}

// NewPromoter returns a Promoter for the cache and saver. Both must be
// non-nil; constructor panics on nil cache (the saver is checked at
// each Run call to allow late wiring during startup).
func NewPromoter(cache *Cache, save PromotionSaver) *Promoter {
	if cache == nil {
		panic("qacache.NewPromoter: nil cache")
	}
	return &Promoter{cache: cache, save: save}
}

// RunOnce promotes all currently-eligible candidates. Returns the count
// of successfully promoted entries. A saver failure on one entry does
// not block the others; the failure is logged and the offending entry
// stays in the cache for the next pass.
func (p *Promoter) RunOnce(ctx context.Context) (int, error) {
	if p == nil || p.cache == nil {
		return 0, nil
	}
	if p.save == nil {
		return 0, errors.New("qacache.Promoter.RunOnce: nil saver")
	}
	candidates := p.cache.PromotionCandidates(time.Now())
	promoted := 0
	for _, e := range candidates {
		if err := p.save(ctx, e); err != nil {
			slog.Warn("qacache: promotion save failed", "key", e.Key, "error", err)
			continue
		}
		p.cache.MarkPromoted(e.Key)
		promoted++
	}
	return promoted, nil
}

// Start runs RunOnce on the given interval until ctx is canceled.
// A best-effort pass happens immediately so short-lived sessions get
// at least one promotion. Returns once ctx is done.
func (p *Promoter) Start(ctx context.Context, interval time.Duration) {
	if p == nil || p.cache == nil {
		return
	}
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if n, err := p.RunOnce(ctx); err != nil {
		slog.Warn("qacache: initial promotion failed", "error", err)
	} else if n > 0 {
		slog.Info("qacache: promoted entries", "count", n)
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if n, err := p.RunOnce(ctx); err != nil {
				slog.Warn("qacache: promotion failed", "error", err)
			} else if n > 0 {
				slog.Info("qacache: promoted entries", "count", n)
			}
		}
	}
}

// PromotedMemoryName returns a deterministic name for a promoted memory
// derived from the entry key. Public so callers (savers) can construct
// idempotent saves — calling promote twice on the same entry overwrites
// rather than duplicates.
func PromotedMemoryName(e *Entry) string {
	return fmt.Sprintf("qa_%s", e.Key)
}
