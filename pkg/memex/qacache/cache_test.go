package qacache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalize_Idempotent(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	a := Normalize("What did we do today?", now)
	b := Normalize("what did we do today!", now)
	if a.Key != b.Key {
		t.Errorf("Normalize should be idempotent across punctuation/case: %q vs %q", a.Key, b.Key)
	}
	if a.Canonical != "what did we do 2026-05-15" {
		t.Errorf("Canonical = %q", a.Canonical)
	}
	if len(a.DateTokens) != 1 || a.DateTokens[0] != "2026-05-15" {
		t.Errorf("DateTokens = %v", a.DateTokens)
	}
}

func TestNormalize_RelativeTimeResolution(t *testing.T) {
	// Two questions on different days should produce different keys.
	t1 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	a := Normalize("what did we do yesterday", t1)
	b := Normalize("what did we do yesterday", t2)
	if a.Key == b.Key {
		t.Errorf("yesterday-of-different-days should differ: %q == %q", a.Key, b.Key)
	}
}

func TestClassify(t *testing.T) {
	// Classify takes the raw (pre-normalization) question so relative-
	// time tokens survive. Confirmed via direct call below.
	cases := []struct {
		q    string
		want QuestionClass
	}{
		{"what did we do today", ClassTimeRelative},
		{"recent changes", ClassTimeRelative},
		{"what happened on 2026-05-14", ClassTimeRelative},
		{"what is the build command", ClassReference},
		{"how do i run tests", ClassReference},
		{"why did we choose bleve", ClassDecision},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			if got := Classify(tc.q); got != tc.want {
				t.Errorf("Classify(%q) = %q, want %q", tc.q, got, tc.want)
			}
		})
	}
}

func TestCache_RecordAndLookup(t *testing.T) {
	dir := t.TempDir()
	c, err := New(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	c.Record("what's our build command", "make build", now, []string{"Makefile"}, []string{"file:Makefile"})

	// Hit.
	got := c.Lookup("What's our build command!", now)
	if got == nil {
		t.Fatal("expected hit")
	}
	if got.Answer != "make build" {
		t.Errorf("Answer = %q", got.Answer)
	}
	if got.AskCount != 2 {
		t.Errorf("AskCount = %d, want 2 (record=1 + lookup=1)", got.AskCount)
	}

	// Persists to disk.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("disk entries: got %d, want 1", len(entries))
	}

	// New cache from same dir reloads.
	c2, err := New(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := c2.Lookup("what's our build command", now); got == nil || got.Answer != "make build" {
		t.Errorf("reload lookup failed: %+v", got)
	}
}

func TestCache_LookupAcrossMidnight(t *testing.T) {
	c, _ := New(t.TempDir())
	day1 := time.Date(2026, 5, 15, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 16, 0, 1, 0, 0, time.UTC)
	c.Record("what did we do today", "fixed the parser", day1, nil, nil)

	// Same question, two minutes later but past midnight, should still
	// hit via ±1-day fuzzy match.
	got := c.Lookup("what did we do today", day2)
	if got == nil {
		t.Fatal("expected midnight-fuzzy hit")
	}
	if got.Answer != "fixed the parser" {
		t.Errorf("Answer = %q", got.Answer)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	c, _ := New(t.TempDir())
	t0 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	c.Record("what did we do today", "x", t0, nil, nil)

	// Just before TTL expires.
	in := t0.Add(2*time.Hour - time.Minute)
	if got := c.Lookup("what did we do today", in); got == nil {
		t.Fatal("expected hit just before expiry")
	}
	// Past TTL.
	out := t0.Add(2*time.Hour + time.Minute)
	if got := c.Lookup("what did we do today", out); got != nil {
		t.Errorf("expected miss after expiry, got %+v", got)
	}
}

func TestCache_InvalidateByEntities(t *testing.T) {
	c, _ := New(t.TempDir())
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	c.Record("a", "a-ans", now, []string{"Makefile"}, nil)
	c.Record("b", "b-ans", now, []string{"main.go"}, nil)
	c.Record("c", "c-ans", now, nil, nil)

	n := c.InvalidateByEntities([]string{"Makefile"})
	if n != 1 {
		t.Errorf("invalidated %d, want 1", n)
	}
	if got := c.Lookup("a", now); got != nil {
		t.Errorf("a should be invalidated")
	}
	if got := c.Lookup("b", now); got == nil {
		t.Errorf("b should survive")
	}
}

func TestCache_PromotionCandidates(t *testing.T) {
	c, _ := New(t.TempDir())
	t0 := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	// Asked twice, ≥1 day old: candidate.
	c.Record("a", "ans", t0, nil, nil)
	c.Lookup("a", t0)
	// Asked once, ≥1 day old: NOT a candidate (need 2+ asks).
	c.Record("b", "ans", t0, nil, nil)
	// Asked twice, fresh: NOT a candidate (need ≥1 day).
	t1 := time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC) // 23h later
	c.Record("c", "ans", t1, nil, nil)
	c.Lookup("c", t1)

	cands := c.PromotionCandidates(time.Date(2026, 5, 16, 13, 0, 0, 0, time.UTC))
	if len(cands) != 1 || cands[0].Canonical != "a" {
		t.Errorf("candidates: got %v", cands)
	}

	c.MarkPromoted(cands[0].Key)
	if got := c.Lookup("a", t0.Add(time.Hour)); got != nil {
		t.Errorf("post-promotion entry should be removed")
	}
	stats := c.Stats()
	if stats.Promoted != 1 {
		t.Errorf("Promoted = %d, want 1", stats.Promoted)
	}
}

func TestCache_PersistsAtomically(t *testing.T) {
	dir := t.TempDir()
	c, _ := New(dir)
	c.Record("q", "a", time.Now(), nil, nil)
	// No half-written file with .tmp suffix should leak.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			t.Errorf("unexpected file in cache dir: %s", e.Name())
		}
	}
}
