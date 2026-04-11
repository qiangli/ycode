package otel

// ModelPricing holds per-million-token pricing for a model.
type ModelPricing struct {
	InputPerMillion      float64
	OutputPerMillion     float64
	CacheWritePerMillion float64
	CacheReadPerMillion  float64
}

// pricingTable maps model prefixes to their pricing.
// Prices are in USD per million tokens.
var pricingTable = map[string]ModelPricing{
	// Claude 4.x / Opus
	"claude-opus-4": {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheWritePerMillion: 18.75, CacheReadPerMillion: 1.50},
	// Claude 4.x / Sonnet
	"claude-sonnet-4": {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheWritePerMillion: 3.75, CacheReadPerMillion: 0.30},
	// Claude 3.5 Sonnet (legacy)
	"claude-3-5-sonnet": {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheWritePerMillion: 3.75, CacheReadPerMillion: 0.30},
	// Claude Haiku
	"claude-haiku-4":   {InputPerMillion: 0.80, OutputPerMillion: 4.0, CacheWritePerMillion: 1.0, CacheReadPerMillion: 0.08},
	"claude-3-5-haiku": {InputPerMillion: 0.80, OutputPerMillion: 4.0, CacheWritePerMillion: 1.0, CacheReadPerMillion: 0.08},
	// GPT-4o
	"gpt-4o": {InputPerMillion: 2.50, OutputPerMillion: 10.0},
	// GPT-4o mini
	"gpt-4o-mini": {InputPerMillion: 0.15, OutputPerMillion: 0.60},
	// GPT-4.1
	"gpt-4.1": {InputPerMillion: 2.0, OutputPerMillion: 8.0},
	// GPT-4.1 mini
	"gpt-4.1-mini": {InputPerMillion: 0.40, OutputPerMillion: 1.60},
	// GPT-4.1 nano
	"gpt-4.1-nano": {InputPerMillion: 0.10, OutputPerMillion: 0.40},
}

// defaultPricing is used when no model prefix matches.
var defaultPricing = ModelPricing{
	InputPerMillion:      3.0,
	OutputPerMillion:     15.0,
	CacheWritePerMillion: 3.75,
	CacheReadPerMillion:  0.30,
}

// LookupPricing returns pricing for a model by longest prefix match.
func LookupPricing(model string) ModelPricing {
	var bestPrefix string
	var bestPricing ModelPricing
	for prefix, pricing := range pricingTable {
		if len(model) >= len(prefix) && model[:len(prefix)] == prefix {
			if len(prefix) > len(bestPrefix) {
				bestPrefix = prefix
				bestPricing = pricing
			}
		}
	}
	if bestPrefix != "" {
		return bestPricing
	}
	return defaultPricing
}

// EstimateCost computes the estimated cost in USD for a single API call.
func EstimateCost(model string, inputTokens, outputTokens, cacheWrite, cacheRead int) float64 {
	p := LookupPricing(model)
	cost := float64(inputTokens) * p.InputPerMillion / 1_000_000
	cost += float64(outputTokens) * p.OutputPerMillion / 1_000_000
	cost += float64(cacheWrite) * p.CacheWritePerMillion / 1_000_000
	cost += float64(cacheRead) * p.CacheReadPerMillion / 1_000_000
	return cost
}
