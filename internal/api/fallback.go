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
	// Keep the replacement request local: callers may reuse req for later turns.
	activeReq := req
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

		events, errCh := p.Send(ctx, activeReq)

		// Peek at the first event or error to detect immediate failures.
		// For streaming providers, transient errors typically arrive as
		// the first (and only) error before any events.
		err := <-errCh
		if err == nil {
			// Success — return the event stream.
			// Re-wrap: the original errCh is drained, create a new closed one.
			doneCh := make(chan error)
			close(doneCh)
			return events, doneCh
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
					return fallbackModelError(activeReq.Model, err)
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

		// Non-transient error — don't fallback, return immediately.
		resultCh := make(chan error, 1)
		resultCh <- err
		close(resultCh)
		return events, resultCh
	}

	// All providers exhausted.
	resultCh := make(chan error, 1)
	resultCh <- fmt.Errorf("all providers in fallback chain failed or on cooldown")
	close(resultCh)
	emptyCh := make(chan *StreamEvent)
	close(emptyCh)
	return emptyCh, resultCh
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

func fallbackModelError(model string, cause error) (<-chan *StreamEvent, <-chan error) {
	events := make(chan *StreamEvent)
	close(events)
	errs := make(chan error, 1)
	errs <- fmt.Errorf("configured model %q does not exist and no alternate model is configured: %w", model, cause)
	close(errs)
	return events, errs
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
