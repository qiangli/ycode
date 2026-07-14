package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// The server TELLS us when its window reopens. We ignored it and guessed with an
// exponential backoff — retrying too early (earning another 429) or too late (wasting
// wall-clock), while the answer sat in a response header.
func TestRetryAfterIsHonouredInBothLegalForms(t *testing.T) {
	if got := parseRetryAfter("30"); got != 30*time.Second {
		t.Errorf("delta-seconds: got %v, want 30s", got)
	}

	// HTTP-date form.
	future := time.Now().Add(45 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	if got < 40*time.Second || got > 50*time.Second {
		t.Errorf("HTTP-date: got %v, want ~45s", got)
	}

	// A date in the past means "go now", not "wait forever".
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("a past Retry-After returned %v, want 0", got)
	}

	for _, junk := range []string{"", "soon", "-5"} {
		if got := parseRetryAfter(junk); got != 0 {
			t.Errorf("parseRetryAfter(%q) = %v, want 0", junk, got)
		}
	}
}

// A 429 is a fact about the PROVIDER, not about the request that happened to hit it.
//
// The old behaviour retried the ONE failed request and changed nothing else — every
// other call kept firing at the same rate, into the same wall. A run that crossed the
// limit once tended to keep crossing it.
func TestA429SlowsDownEverySubsequentRequestToThatHost(t *testing.T) {
	p := &pacer{hosts: map[string]*hostPace{}}

	// Before any 429, there is no pacing at all: we do not tax a healthy provider.
	start := time.Now()
	if err := p.wait(context.Background(), "api.example.com"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Errorf("a host that never rate-limited us was paced anyway (%v)", elapsed)
	}

	p.penalize("api.example.com", 0)

	hp := p.hosts["api.example.com"]
	if hp.spacing != pacerInitialSpacing {
		t.Fatalf("after one 429, spacing = %v, want %v", hp.spacing, pacerInitialSpacing)
	}

	// A second 429 backs off further — the first correction was not enough.
	p.penalize("api.example.com", 0)
	if hp.spacing != 2*pacerInitialSpacing {
		t.Errorf("after two 429s, spacing = %v, want it doubled to %v", hp.spacing, 2*pacerInitialSpacing)
	}

	// And a DIFFERENT host is untouched: one provider's limit is not another's.
	if _, exists := p.hosts["api.other.com"]; exists {
		t.Error("penalising one host paced an unrelated one")
	}
}

// Spacing is bounded. Past a point the provider is telling us something a pacer cannot
// fix, and slowing to a crawl just converts a rate limit into a hang.
func TestPacingIsBounded(t *testing.T) {
	p := &pacer{hosts: map[string]*hostPace{}}
	for i := 0; i < 20; i++ {
		p.penalize("api.example.com", 0)
	}
	if got := p.hosts["api.example.com"].spacing; got > pacerMaxSpacing {
		t.Errorf("spacing grew to %v, past the %v ceiling", got, pacerMaxSpacing)
	}
}

// Retry-After sets a hard floor on WHEN the next request may go out — separately from
// the spacing, which governs the rate afterwards.
func TestRetryAfterSetsTheEarliestNextRequest(t *testing.T) {
	p := &pacer{hosts: map[string]*hostPace{}}
	p.penalize("api.example.com", 2*time.Second)

	hp := p.hosts["api.example.com"]
	if until := time.Until(hp.notBefore); until < time.Second || until > 3*time.Second {
		t.Errorf("Retry-After: next request allowed in %v, want ~2s", until)
	}
}

// The pacer must respect cancellation. A rate limit must never become a hang.
func TestPacerRespectsContextCancellation(t *testing.T) {
	p := &pacer{hosts: map[string]*hostPace{}}
	p.penalize("api.example.com", 30*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.wait(ctx, "api.example.com")
	if err == nil {
		t.Fatal("pacer waited out a 30s penalty despite a cancelled context")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("pacer took %v to notice cancellation", elapsed)
	}
}
