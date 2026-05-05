package memory

import (
	"math"
	"testing"
	"time"
)

func TestRecencyDecay(t *testing.T) {
	halfLife := 30 * 24 * time.Hour

	// A memory updated just now should have decay close to 1.0.
	recent := time.Now()
	got := RecencyDecay(recent, halfLife)
	if got < 0.99 {
		t.Errorf("RecencyDecay(now) = %f, want >= 0.99", got)
	}

	// A memory updated exactly one half-life ago should decay to ~0.5.
	old := time.Now().Add(-halfLife)
	got = RecencyDecay(old, halfLife)
	if math.Abs(got-0.5) > 0.05 {
		t.Errorf("RecencyDecay(1 half-life ago) = %f, want ~0.5", got)
	}

	// A memory updated two half-lives ago should decay to ~0.25.
	veryOld := time.Now().Add(-2 * halfLife)
	got = RecencyDecay(veryOld, halfLife)
	if math.Abs(got-0.25) > 0.05 {
		t.Errorf("RecencyDecay(2 half-lives ago) = %f, want ~0.25", got)
	}

	// Future time should return 1.0.
	future := time.Now().Add(time.Hour)
	got = RecencyDecay(future, halfLife)
	if got != 1.0 {
		t.Errorf("RecencyDecay(future) = %f, want 1.0", got)
	}
}

func TestCompositeScore(t *testing.T) {
	weights := DefaultWeights()

	// All components at maximum.
	now := time.Now()
	got := CompositeScore(1.0, now, 1.0, weights)
	// Expected: 0.5*1.0 + 0.3*~1.0 + 0.2*1.0 = ~1.0
	if got < 0.95 || got > 1.05 {
		t.Errorf("CompositeScore(max) = %f, want ~1.0", got)
	}

	// Zero similarity, recent, default importance.
	got = CompositeScore(0.0, now, 0.0, weights)
	// Expected: 0.5*0 + 0.3*~1.0 + 0.2*0.5 = ~0.4
	if got < 0.35 || got > 0.45 {
		t.Errorf("CompositeScore(zero similarity, default importance) = %f, want ~0.4", got)
	}

	// Negative importance should default to 0.5.
	got = CompositeScore(0.5, now, -1.0, weights)
	expected := 0.5*0.5 + 0.3*1.0 + 0.2*0.5 // ~0.65
	if math.Abs(got-expected) > 0.05 {
		t.Errorf("CompositeScore(negative importance) = %f, want ~%f", got, expected)
	}
}

func TestDefaultWeights(t *testing.T) {
	w := DefaultWeights()
	sum := w.Semantic + w.Recency + w.Importance
	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("DefaultWeights sum = %f, want 1.0", sum)
	}
}
