package gaptracker

import (
	"context"
	"testing"

	"github.com/qiangli/ycode/internal/runtime/toolexec"
)

func TestTracker_Dedup(t *testing.T) {
	// Create tracker with nil client (won't actually call Gitea).
	tracker := &Tracker{
		seen:     make(map[string]*gap),
		stopCh:   make(chan struct{}),
		debounce: 0,
	}

	ctx := context.Background()

	// Record same gap twice.
	tracker.RecordGap(ctx, "git", "stash", toolexec.TierHostExec)
	tracker.RecordGap(ctx, "git", "stash", toolexec.TierHostExec)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	g, ok := tracker.seen["git:stash"]
	if !ok {
		t.Fatal("expected gap to be recorded")
	}
	if g.Count != 2 {
		t.Errorf("expected count 2, got %d", g.Count)
	}
	if len(tracker.pending) != 1 {
		t.Errorf("expected 1 pending gap (deduped), got %d", len(tracker.pending))
	}
}

func TestTracker_TierUpgrade(t *testing.T) {
	tracker := &Tracker{
		seen:     make(map[string]*gap),
		stopCh:   make(chan struct{}),
		debounce: 0,
	}

	ctx := context.Background()

	// First record at tier 2.
	tracker.RecordGap(ctx, "git", "stash", toolexec.TierHostExec)

	// Second record at tier 3 (higher).
	tracker.RecordGap(ctx, "git", "stash", toolexec.TierContainer)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	g := tracker.seen["git:stash"]
	if g.Tier != toolexec.TierContainer {
		t.Errorf("expected tier upgrade to TierContainer, got %v", g.Tier)
	}
}

func TestTracker_DifferentGaps(t *testing.T) {
	tracker := &Tracker{
		seen:     make(map[string]*gap),
		stopCh:   make(chan struct{}),
		debounce: 0,
	}

	ctx := context.Background()

	tracker.RecordGap(ctx, "git", "stash", toolexec.TierHostExec)
	tracker.RecordGap(ctx, "git", "worktree", toolexec.TierContainer)
	tracker.RecordGap(ctx, "docker", "build", toolexec.TierHostExec)

	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	if len(tracker.seen) != 3 {
		t.Errorf("expected 3 gaps, got %d", len(tracker.seen))
	}
	if len(tracker.pending) != 3 {
		t.Errorf("expected 3 pending, got %d", len(tracker.pending))
	}
}

func TestParseIssueTitle(t *testing.T) {
	tests := []struct {
		title   string
		wantCat string
		wantSub string
	}{
		{"[capability-gap][git] stash: no native implementation", "git", "stash"},
		{"[capability-gap][docker] build: no native implementation", "docker", "build"},
		{"[capability-gap][git] worktree", "git", "worktree"},
		{"random issue title", "", ""},
		{"[capability-gap]malformed", "", ""},
	}

	for _, tt := range tests {
		cat, sub := parseIssueTitle(tt.title)
		if cat != tt.wantCat || sub != tt.wantSub {
			t.Errorf("parseIssueTitle(%q) = (%q, %q), want (%q, %q)",
				tt.title, cat, sub, tt.wantCat, tt.wantSub)
		}
	}
}
