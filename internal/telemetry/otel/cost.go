package otel

import "github.com/qiangli/ycode/internal/runtime/usage"

// ModelPricing holds per-million-token pricing for a model.
// This type is kept for backward compatibility. The canonical pricing data
// lives in usage.PricingTable (internal/runtime/usage/pricing.go).
type ModelPricing struct {
	InputPerMillion      float64
	OutputPerMillion     float64
	CacheWritePerMillion float64
	CacheReadPerMillion  float64
}

// LookupPricing returns pricing for a model by longest prefix match.
// Delegates to usage.LookupPricing — the single source of truth for pricing data.
func LookupPricing(model string) ModelPricing {
	p := usage.LookupPricing(model)
	return ModelPricing{
		InputPerMillion:      p.InputPerM,
		OutputPerMillion:     p.OutputPerM,
		CacheWritePerMillion: p.CacheWritePerM,
		CacheReadPerMillion:  p.CacheReadPerM,
	}
}

// EstimateCost computes the estimated cost in USD for a single API call.
// Delegates to usage.EstimateCost — the single source of truth for pricing data.
func EstimateCost(model string, inputTokens, outputTokens, cacheWrite, cacheRead int) float64 {
	return usage.EstimateCost(model, inputTokens, outputTokens, cacheWrite, cacheRead)
}
