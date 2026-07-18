package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// FallbackProvider wraps multiple providers and falls back on transient errors.
type FallbackProvider struct {
	providers         []Provider
	configs           []ProviderConfig
	fallbackModelName string
	mu                sync.RWMutex
	cooldowns         map[int]time.Time // index → cooldown expiry
	logger            *slog.Logger
}

// FallbackConfig configures the fallback chain.
type FallbackConfig struct {
	Providers     []ProviderConfig
	FallbackModel string // alternate model to try after a ModelNotFound response
	Logger        *slog.Logger
}

// NewFallbackProvider creates a provider that tries each provider in order,
// falling back on rate limits (429), server errors (5xx), or timeouts.
// If only one provider is configured, it behaves identically to a direct provider.
func NewFallbackProvider(cfg FallbackConfig) (*FallbackProvider, error) {
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("fallback chain requires at least one provider")
	}

	providers := make([]Provider, len(cfg.Providers))
	for i, pc := range cfg.Providers {
		providers[i] = NewProvider(&pc)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &FallbackProvider{
		providers:         providers,
		configs:           cfg.Providers,
		fallbackModelName: cfg.FallbackModel,
		cooldowns:         make(map[int]time.Time),
		logger:            logger,
	}, nil
}

// Send tries each provider in order. On transient failure, it falls back.
func (fp *FallbackProvider) Send(ctx context.Context, req *Request) (<-chan *StreamEvent, <-chan error) {
	outEvents := make(chan *StreamEvent, 64)
	outErrs := make(chan error, 1)
	go fp.send(ctx, req, outEvents, outErrs)
	return outEvents, outErrs
}

// send owns the fallback state machine and forwards the selected provider's
// stream as it arrives. Provider error channels are terminal: an attempt may
// fall back only when it fails before exposing any event. Once callers have
// observed part of a response, replaying the request against another provider
// would splice two model responses together and can duplicate tool calls.
func (fp *FallbackProvider) send(ctx context.Context, req *Request, outEvents chan<- *StreamEvent, outErrs chan<- error) {
	defer close(outEvents)
	defer close(outErrs)

	// Keep the replacement request local: callers may reuse req for later turns.
	activeReq := req
	var lastErr error // the most recent provider failure, surfaced if the chain collapses
	for i := 0; i < len(fp.providers); i++ {
		p := fp.providers[i]
		// Skip providers on cooldown.
		if fp.isOnCooldown(i) {
			fp.logger.Debug("skipping provider on cooldown",
				"index", i,
				"provider", fp.configs[i].DisplayKind(),
			)
			continue
		}

		attemptCtx, cancelAttempt := context.WithCancel(ctx)
		events, errCh := p.Send(attemptCtx, activeReq)
		emitted, err := relayProviderAttempt(attemptCtx, events, errCh, outEvents)
		cancelAttempt()
		if err == nil {
			return
		}
		lastErr = err
		if emitted {
			outErrs <- err
			return
		}
		if ctx.Err() != nil {
			outErrs <- ctx.Err()
			return
		}

		// Use ClassifiedError for smart recovery when available.
		var classifiedErr *ClassifiedError
		if errors.As(err, &classifiedErr) {
			switch classifiedErr.Action {
			case ActionRetry:
				fp.setCooldown(i, 60*time.Second)
				fp.logger.Warn("provider failed, trying fallback",
					"index", i,
					"provider", fp.configs[i].DisplayKind(),
					"reason", classifiedErr.Reason.String(),
				)
				continue
			case ActionRotateKey:
				// Put this provider on longer cooldown (auth issue).
				fp.setCooldown(i, 5*time.Minute)
				fp.logger.Warn("provider auth error, rotating to fallback",
					"index", i,
					"provider", fp.configs[i].DisplayKind(),
					"reason", classifiedErr.Reason.String(),
				)
				continue
			case ActionFallbackModel:
				fallbackModel := fp.fallbackModel(activeReq.Model)
				if fallbackModel == "" || fallbackModel == activeReq.Model {
					outErrs <- fmt.Errorf("configured model %q does not exist and no alternate model is configured: %w", activeReq.Model, err)
					return
				}
				fp.logger.Warn("configured model not found; retrying with fallback model",
					"index", i,
					"provider", fp.configs[i].DisplayKind(),
					"configured_model", activeReq.Model,
					"fallback_model", fallbackModel,
				)
				fallbackReq := *activeReq
				fallbackReq.Model = fallbackModel
				activeReq = &fallbackReq
				// A missing model is not a provider health failure. Retry this
				// provider with the configured known-good model before moving on.
				i--
				continue
			default:
				// ActionAbort, ActionCompressContext — return to caller.
			}
		} else if isTransientError(err) {
			// Legacy fallback for non-classified errors.
			fp.setCooldown(i, 60*time.Second)
			fp.logger.Warn("provider failed with transient error, trying fallback",
				"index", i,
				"provider", fp.configs[i].DisplayKind(),
				"error", err,
			)
			continue
		}

		// Non-transient error — don't fallback, return immediately. A PERMANENT auth
		// failure (401/403) gets a CLEAR error that names the provider and fingerprints
		// the key (last 4), so a stale/invalid key is obvious at a glance instead of an
		// opaque abort — and the run fails FAST here rather than hanging (the preflight
		// probe in preflight.go catches most of these before a run even starts).
		if sc := statusCodeOf(err); sc == 401 || sc == 403 {
			authErr := NewAuthError(fp.configs[i].DisplayKind(), sc, fp.configs[i].credential())
			fp.logger.Warn("provider rejected its API key; failing fast",
				"provider", fp.configs[i].DisplayKind(),
				"key", authErr.Fingerprint,
			)
			outErrs <- authErr
			return
		}
		outErrs <- err
		return
	}

	// All providers exhausted. Carry the last failure so the operator can see WHY the
	// chain collapsed instead of guessing at cooldowns.
	if lastErr != nil {
		outErrs <- fmt.Errorf("all providers in fallback chain failed or on cooldown: %w", lastErr)
	} else {
		outErrs <- fmt.Errorf("all providers in fallback chain failed or on cooldown")
	}
}

// statusCodeOf extracts an HTTP status code from a classified provider error.
func statusCodeOf(err error) int {
	var ce *ClassifiedError
	if errors.As(err, &ce) {
		return ce.StatusCode
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// relayProviderAttempt drains both channels concurrently. The old fallback
// implementation waited for errCh before returning events; OpenAI-compatible
// providers close errCh only after finishing the stream, so their 64-event
// buffer deadlocked on event 65. Forwarding here preserves streaming and
// provides backpressure from the actual caller instead of the wrapper buffer.
func relayProviderAttempt(ctx context.Context, events <-chan *StreamEvent, errs <-chan error, out chan<- *StreamEvent) (bool, error) {
	emitted := false
	var terminalErr error
	for events != nil || errs != nil {
		select {
		case <-ctx.Done():
			return emitted, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			select {
			case out <- ev:
				emitted = true
			case <-ctx.Done():
				return emitted, ctx.Err()
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil && terminalErr == nil {
				terminalErr = err
			}
		}
	}
	return emitted, terminalErr
}

// fallbackModel yields the next model to try after a ModelNotFound. It walks an
// ordered chain: for an UNTAGGED request it tries "<model>:latest" first — the
// Ollama / cloudbox-pool convention tags every model, so an untagged name
// ("gpt-oss-20b") 404s where "gpt-oss-20b:latest" is served — then falls through
// to the configured known-good default. The `requested != fallbackModelName`
// guard keeps it from appending ":latest" to the default itself, so the chain
// always terminates (…":latest" carries a ":", the default returns itself → the
// caller sees fallback == requested and errors out).
func (fp *FallbackProvider) fallbackModel(requested string) string {
	if requested == "" {
		return ""
	}
	if !strings.Contains(requested, ":") && requested != fp.fallbackModelName {
		return requested + ":latest"
	}
	return fp.fallbackModelName
}

// Kind returns the kind of the first (primary) provider.
func (fp *FallbackProvider) Kind() ProviderKind {
	if len(fp.providers) > 0 {
		return fp.providers[0].Kind()
	}
	return ProviderAnthropic
}

func (fp *FallbackProvider) isOnCooldown(idx int) bool {
	fp.mu.RLock()
	defer fp.mu.RUnlock()
	expiry, ok := fp.cooldowns[idx]
	if !ok {
		return false
	}
	return time.Now().Before(expiry)
}

func (fp *FallbackProvider) setCooldown(idx int, duration time.Duration) {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	fp.cooldowns[idx] = time.Now().Add(duration)
}

// isTransientError checks if an error is retryable (rate limit, server error, timeout).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// HTTP status code patterns.
	if strings.Contains(msg, "429") || strings.Contains(msg, "rate limit") || strings.Contains(msg, "Rate limit") {
		return true
	}
	if strings.Contains(msg, "500") || strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") || strings.Contains(msg, "504") {
		return true
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "Timeout") {
		return true
	}
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "connection reset") {
		return true
	}
	return false
}
