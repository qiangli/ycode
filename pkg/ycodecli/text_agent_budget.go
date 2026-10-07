package ycodecli

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/qiangli/ycode/pkg/ycode"
	"github.com/qiangli/yoke/pkg/llmbudget"
)

// Meter every transport attempt, including YAML retries and fallbacks. Budget
// redirects are refusals here: only the compiled YAML may select another route.
func agentFenceMeterProvider(ref string, backend ycode.Provider) ycode.Provider {
	return &agentFenceMeter{Provider: backend, ref: ref}
}

type agentFenceMeter struct {
	ycode.Provider
	ref string
}

func (p *agentFenceMeter) Send(ctx context.Context, request *ycode.ProviderRequest) (<-chan *ycode.ProviderStreamEvent, <-chan error) {
	out := make(chan *ycode.ProviderStreamEvent)
	fail := make(chan error, 1)
	gate := llmbudget.DefaultGate()
	owner, err := gate.NewOwner(ctx, "agent YAML provider "+p.ref)
	if err != nil {
		fail <- err
		close(fail)
		close(out)
		return out, fail
	}
	id := "agent-yaml-" + owner.ID()
	admission, err := gate.Reserve(ctx, llmbudget.Request{ID: id, Owner: owner.ID(), Model: request.Model,
		UnknownTokens: true, Concurrency: 1, TTL: 2 * time.Minute})
	if err != nil || admission.Decision.Action != llmbudget.Allow || admission.Reservation == nil {
		owner.Close()
		if err == nil {
			err = fmt.Errorf("agent YAML budget %s: %s", admission.Decision.Action, admission.Decision.Reason)
		}
		fail <- err
		close(fail)
		close(out)
		return out, fail
	}
	ctx, cancel := context.WithCancel(ctx)
	events, failures := p.Provider.Send(ctx, request)
	go func() {
		defer cancel()
		defer close(out)
		defer close(fail)
		defer owner.Close()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var input, output, cached int64
		var observed bool
		var providerErr error
		for events != nil || failures != nil {
			select {
			case <-ctx.Done():
				// An interrupted remote request has no proven final usage. Keep
				// its reservation for the existing reconciliation authority.
				fail <- ctx.Err()
				return
			case <-ticker.C:
				if err := gate.Renew(ctx, id, owner.ID(), 2*time.Minute); err != nil {
					fail <- err
					return
				}
			case err, ok := <-failures:
				if !ok {
					failures = nil
					continue
				}
				if err != nil {
					providerErr = err
				}
			case item, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				if item != nil {
					usage := item.Usage
					if usage == nil && item.Message != nil {
						usage = &item.Message.Usage
					}
					if usage != nil {
						in := int64(max(usage.InputTokens, usage.PromptTokens))
						out := int64(max(usage.OutputTokens, usage.CompletionTokens))
						input = max(input, in)
						output = max(output, out)
						cached = max(cached, int64(usage.CacheReadInput))
						if usage.PromptTokensDetails != nil {
							cached = max(cached, int64(usage.PromptTokensDetails.CachedTokens))
						}
						observed = observed || in > 0 || out > 0
					}
				}
				select {
				case out <- item:
				case <-ctx.Done():
					fail <- ctx.Err()
					return
				}
			}
		}
		if providerErr != nil {
			fail <- providerErr
			return
		}
		actual := llmbudget.Actual{InputTokens: input, OutputTokens: output, CachedInputTokens: cached,
			Source: "ycode-provider-usage", ObservedAt: time.Now().UTC()}
		if !observed {
			// Preserve the absence of a receipt explicitly. Never claim this
			// estimate establishes a hard ceiling or actual provider spend.
			raw, _ := json.Marshal(request)
			actual.InputTokens = int64(len(raw)/4 + 1)
			actual.TokensEstimated = true
			actual.Source = "ycode-request-estimate-no-usage-receipt"
		}
		if cost, known := llmbudget.EstimatedCostUSD(request.Model, actual.InputTokens+actual.OutputTokens); known && cost >= 0 && cost < float64(math.MaxInt64)/1e6 {
			micro := int64(math.Ceil(cost * 1e6))
			actual.SpendMicroUSD = &micro
		}
		settleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := gate.Settle(settleCtx, id, owner.ID(), actual); err != nil {
			fail <- err
		}
	}()
	return out, fail
}
