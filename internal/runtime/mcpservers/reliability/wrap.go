// Package reliability ports the openchrome (MIT) reliability
// primitives to Go and applies them uniformly to every browser mode
// (live, probe, solo) via Wrap. The primitives are intentionally
// independent so each can be toggled off via Config.
//
// Source attribution:
//   - Hint Engine, Ralph fallback, Circuit breaker, DOM
//     compression, Pattern Learner, Outcome Classifier — design
//     ideas from https://github.com/shaun0927/openchrome (MIT).
//     This Go port is an independent reimplementation with the
//     same rule names and thresholds.
package reliability

import (
	"context"

	"github.com/qiangli/ycode/internal/runtime/mcpservers"
)

// Config gates each primitive independently. nil-valued bool
// pointers mean "default on"; explicit false disables.
type Config struct {
	HintEngine     *bool
	RalphFallback  *bool
	CircuitBreaker *bool
	CompactDOM     *bool
	PatternLearner *bool
	// OutcomeClassifier piggybacks on HintEngine — same toggle.
}

func enabled(p *bool) bool {
	if p == nil {
		return true
	}
	return *p
}

// Wrap layers the configured primitives around a Service. The
// wrappers compose pre-call (circuit breaker, Ralph) and post-call
// (DOM compression, Hint Engine + Outcome Classifier, Pattern
// Learner) so a single Execute path produces an annotated result.
func Wrap(svc mcpservers.Service, cfg Config) mcpservers.Service {
	if svc == nil {
		return nil
	}
	out := svc
	if enabled(cfg.CompactDOM) {
		out = &compactDOMWrapper{inner: out}
	}
	if enabled(cfg.HintEngine) {
		out = &hintEngineWrapper{inner: out}
	}
	if enabled(cfg.RalphFallback) {
		out = &ralphWrapper{inner: out}
	}
	if enabled(cfg.CircuitBreaker) {
		out = &breakerWrapper{inner: newBreaker(), next: out}
	}
	if enabled(cfg.PatternLearner) {
		out = newLearnerWrapper(out)
	}
	return out
}

// passthrough is a tiny helper used by every wrapper to forward
// non-Execute methods to the inner service.
type passthrough struct{ inner mcpservers.Service }

func (p *passthrough) Name() string                       { return p.inner.Name() }
func (p *passthrough) Available(ctx context.Context) bool { return p.inner.Available(ctx) }
func (p *passthrough) EnsureReady(ctx context.Context) error {
	return p.inner.EnsureReady(ctx)
}
func (p *passthrough) Stop(ctx context.Context) error { return p.inner.Stop(ctx) }
