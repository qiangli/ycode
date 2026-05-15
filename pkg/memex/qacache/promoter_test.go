package qacache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPromoter_RunOnce(t *testing.T) {
	c, _ := New(t.TempDir())
	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	c.Record("why did we pick bleve", "for pure-Go full-text", t0, nil, nil)
	c.Lookup("why did we pick bleve", t0)

	saved := []*Entry{}
	saver := func(_ context.Context, e *Entry) error {
		saved = append(saved, e)
		return nil
	}
	p := NewPromoter(c, saver)

	// Run with "now" 25h later — entry is eligible (≥2 asks, ≥1 day old).
	now := t0.Add(25 * time.Hour)
	defer func(orig func() time.Time) {}(time.Now)
	got, err := p.runOnceAt(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got != 1 || len(saved) != 1 {
		t.Errorf("promoted = %d (saved=%d), want 1", got, len(saved))
	}
	if saved[0].AskCount < 2 {
		t.Errorf("AskCount on saved entry = %d", saved[0].AskCount)
	}
	if c.Stats().Promoted != 1 {
		t.Errorf("Promoted counter = %d", c.Stats().Promoted)
	}
	// Re-run should find no candidates.
	got, _ = p.runOnceAt(context.Background(), now)
	if got != 0 {
		t.Errorf("re-run promoted = %d, want 0", got)
	}
}

func TestPromoter_SaverFailureKeepsEntry(t *testing.T) {
	c, _ := New(t.TempDir())
	t0 := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	c.Record("a", "a-ans", t0, nil, nil)
	c.Lookup("a", t0)

	failing := func(_ context.Context, _ *Entry) error {
		return errors.New("boom")
	}
	p := NewPromoter(c, failing)
	got, err := p.runOnceAt(context.Background(), t0.Add(25*time.Hour))
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got != 0 {
		t.Errorf("promoted on save-fail = %d, want 0", got)
	}
	if c.Stats().Promoted != 0 {
		t.Errorf("Promoted counter incremented despite save failure")
	}
	// Entry still present.
	if e := c.Lookup("a", t0.Add(25*time.Hour)); e == nil {
		t.Errorf("entry should remain after save failure")
	}
}

// runOnceAt is a test seam that lets us pin "now" without monkey-patching
// time.Now. Mirrors RunOnce.
func (p *Promoter) runOnceAt(ctx context.Context, now time.Time) (int, error) {
	if p == nil || p.cache == nil {
		return 0, nil
	}
	if p.save == nil {
		return 0, errors.New("nil saver")
	}
	cands := p.cache.PromotionCandidates(now)
	n := 0
	for _, e := range cands {
		if err := p.save(ctx, e); err != nil {
			continue
		}
		p.cache.MarkPromoted(e.Key)
		n++
	}
	return n, nil
}
