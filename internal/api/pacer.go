package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A rate limit is a fact about the PROVIDER, not about the work.
//
// ycode used to react to a 429 by retrying the ONE request that failed, with a blind
// exponential backoff, and changing nothing else. Every other in-flight request kept
// firing at the same rate, straight into the same wall. So a run that crossed the limit
// once tended to keep crossing it — a steady drizzle of 429s, each individually
// recovered, collectively costing minutes.
//
// Worse, when retries were exhausted the error surfaced INTO THE AGENT'S CONTEXT, and
// the agent — reasonably — tried to solve it. Measured, on a real run: glm-5.2 read
// "rate limit" in a tool result and issued `sleep 120`. Three of its turns went to
// napping. It was doing the harness's job, badly, with the operator's iteration budget.
//
// The agent must never see a 429. Pacing is plumbing.

// providerPacer is the process-wide pacer, keyed by provider host.
var providerPacer = &pacer{hosts: map[string]*hostPace{}}

// hostPace is the current pacing state for one provider host.
type hostPace struct {
	// notBefore is the earliest time the next request to this host may be sent.
	notBefore time.Time
	// spacing is the minimum gap we currently impose between requests. It grows on a
	// 429 and decays when the host behaves.
	spacing time.Duration
	// lastSent is when we last released a request to this host.
	lastSent time.Time
	// lastPenalty is when this host last rate-limited us; spacing decays from here.
	lastPenalty time.Time
}

const (
	// pacerInitialSpacing is the gap imposed after the FIRST 429 from a host. Modest:
	// most limits are per-minute, and over-correcting costs wall-clock on every
	// subsequent call.
	pacerInitialSpacing = 750 * time.Millisecond

	// pacerMaxSpacing bounds how slow we will voluntarily go. Past this the provider is
	// telling us something a pacer cannot fix.
	pacerMaxSpacing = 8 * time.Second

	// pacerDecay is how long a host must behave before its spacing halves. Recovery is
	// gradual on purpose: snapping straight back to full speed just re-earns the 429.
	pacerDecay = 30 * time.Second
)

type pacer struct {
	mu    sync.Mutex
	hosts map[string]*hostPace
}

// wait blocks until this host is allowed another request.
//
// It is the ONLY place a rate limit is allowed to cost time, and it costs it in the
// transport, where the agent cannot see it and cannot try to help.
func (p *pacer) wait(ctx context.Context, host string) error {
	p.mu.Lock()
	hp := p.hosts[host]
	if hp == nil || hp.spacing == 0 {
		p.mu.Unlock()
		return nil
	}

	// Decay: a host that has behaved for a while earns its speed back, gradually.
	if since := time.Since(hp.lastPenalty); since > pacerDecay {
		halvings := int(since / pacerDecay)
		for i := 0; i < halvings && hp.spacing > 0; i++ {
			hp.spacing /= 2
			if hp.spacing < 50*time.Millisecond {
				hp.spacing = 0
				break
			}
		}
		hp.lastPenalty = time.Now()
		if hp.spacing == 0 {
			p.mu.Unlock()
			return nil
		}
	}

	now := time.Now()
	earliest := hp.lastSent.Add(hp.spacing)
	if hp.notBefore.After(earliest) {
		earliest = hp.notBefore
	}
	delay := earliest.Sub(now)
	if delay <= 0 {
		hp.lastSent = now
		p.mu.Unlock()
		return nil
	}
	hp.lastSent = earliest
	p.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// penalize records that a host rate-limited us, and slows every subsequent request to it.
//
// serverWait is the Retry-After the server gave us, if any. It is authoritative for WHEN
// to retry; the spacing is what we impose afterwards so we do not immediately re-earn it.
func (p *pacer) penalize(host string, serverWait time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	hp := p.hosts[host]
	if hp == nil {
		hp = &hostPace{}
		p.hosts[host] = hp
	}

	switch {
	case hp.spacing == 0:
		hp.spacing = pacerInitialSpacing
	case hp.spacing < pacerMaxSpacing:
		hp.spacing *= 2
		if hp.spacing > pacerMaxSpacing {
			hp.spacing = pacerMaxSpacing
		}
	}
	hp.lastPenalty = time.Now()

	if serverWait > 0 {
		hp.notBefore = time.Now().Add(serverWait)
	}

	slog.Warn("provider rate-limited us; pacing subsequent requests",
		"host", host,
		"spacing", hp.spacing.Round(time.Millisecond),
		"retry_after", serverWait.Round(time.Millisecond),
	)
}

// parseRetryAfter reads the Retry-After header, in either legal form: delta-seconds, or
// an HTTP-date.
//
// The server knows when its window reopens and we do not. Guessing when the answer is in
// a header is not resilience, it is noise.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// IsQuotaExhausted distinguishes a QUOTA that has run out from a RATE that is too high.
//
// Both arrive as HTTP 429, and treating them the same is how nine retries got burned
// against a limit that would not reset for FIFTEEN HOURS. Measured, on a real run:
//
//	1302  "Rate limit reached for requests"     -> too fast. Back off; it clears in seconds.
//	1308  "Usage limit reached for 5 hour.      -> OUT OF QUOTA. Retrying is futile until
//	       Your limit will reset at <time>"        the window resets. Nine attempts, ~30s of
//	                                               backoff, and it could never have worked.
//
// The distinction is not cosmetic — the two failures want OPPOSITE responses:
//
//	rate limited  -> WAIT. The same agent will succeed shortly.
//	quota gone    -> HAND OFF. This agent cannot succeed at all, for hours. Retrying it
//	                 wastes wall-clock; routing to another agent is the only move that can
//	                 produce an answer.
//
// This is the failure classification that makes a backup chain sane: a backup only helps
// when the failure is AGENT-SPECIFIC. A quota exhaustion is exactly that. A failing gate
// is not — hand THAT down a chain of five agents and you get five identical failures.
func IsQuotaExhausted(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{
		`"code":"1308"`,   // z.ai: usage limit reached for the N-hour window
		"usage limit reached",
		"quota exceeded",
		"insufficient_quota",
		"billing_hard_limit_reached",
		"credit balance is too low",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
