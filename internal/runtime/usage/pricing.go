package usage

// ModelPricing holds per-token costs in USD per million tokens.
type ModelPricing struct {
	InputPerM      float64 // cost per 1M input tokens
	OutputPerM     float64 // cost per 1M output tokens
	CacheWritePerM float64 // cost per 1M cache creation tokens
	CacheReadPerM  float64 // cost per 1M cache read tokens
}

// PricingTable maps model name patterns to pricing.
// Keys use prefix matching — "claude-sonnet" matches "claude-sonnet-4-20250514".
// PricingTable maps model name patterns to pricing.
// Keys use prefix matching — "claude-sonnet" matches "claude-sonnet-4-20250514".
// This is the single source of truth for model pricing. The telemetry/otel
// package delegates to this table via usage.EstimateCost().
var PricingTable = map[string]ModelPricing{
	// Anthropic Claude 4.x
	"claude-opus-4":   {InputPerM: 15.0, OutputPerM: 75.0, CacheWritePerM: 18.75, CacheReadPerM: 1.50},
	"claude-sonnet-4": {InputPerM: 3.0, OutputPerM: 15.0, CacheWritePerM: 3.75, CacheReadPerM: 0.30},
	"claude-haiku-4":  {InputPerM: 0.80, OutputPerM: 4.0, CacheWritePerM: 1.0, CacheReadPerM: 0.08},
	// Anthropic Claude 3.5 (legacy)
	"claude-3-5-sonnet": {InputPerM: 3.0, OutputPerM: 15.0, CacheWritePerM: 3.75, CacheReadPerM: 0.30},
	"claude-3-5-haiku":  {InputPerM: 0.80, OutputPerM: 4.0, CacheWritePerM: 1.0, CacheReadPerM: 0.08},
	// OpenAI GPT-4o
	"gpt-4o":      {InputPerM: 2.50, OutputPerM: 10.0, CacheWritePerM: 0, CacheReadPerM: 1.25},
	"gpt-4o-mini": {InputPerM: 0.15, OutputPerM: 0.60, CacheWritePerM: 0, CacheReadPerM: 0.075},
	// OpenAI GPT-4.1
	"gpt-4.1":      {InputPerM: 2.0, OutputPerM: 8.0},
	"gpt-4.1-mini": {InputPerM: 0.40, OutputPerM: 1.60},
	"gpt-4.1-nano": {InputPerM: 0.10, OutputPerM: 0.40},
	// OpenAI o-series
	"o3":     {InputPerM: 10.0, OutputPerM: 40.0, CacheWritePerM: 0, CacheReadPerM: 5.0},
	"o3-mini": {InputPerM: 1.10, OutputPerM: 4.40, CacheWritePerM: 0, CacheReadPerM: 0.55},
	"o4-mini": {InputPerM: 1.10, OutputPerM: 4.40, CacheWritePerM: 0, CacheReadPerM: 0.55},
	// Google Gemini
	"gemini-2.5-pro":   {InputPerM: 1.25, OutputPerM: 10.0, CacheWritePerM: 0, CacheReadPerM: 0.315},
	"gemini-2.5-flash": {InputPerM: 0.15, OutputPerM: 0.60, CacheWritePerM: 0, CacheReadPerM: 0.0375},
	// Local models (Ollama) — zero cost.
	"local": {InputPerM: 0, OutputPerM: 0},
	// Fallback
	"default": {InputPerM: 3.0, OutputPerM: 15.0, CacheWritePerM: 3.75, CacheReadPerM: 0.30},
}

// LookupPricing finds the best matching pricing for a model name.
// Uses prefix matching, falling back to "default".
func LookupPricing(model string) ModelPricing {
	// Exact match first.
	if p, ok := PricingTable[model]; ok {
		return p
	}
	// Prefix match (longest prefix wins).
	bestKey := ""
	for key := range PricingTable {
		if key == "default" {
			continue
		}
		if len(model) >= len(key) && model[:len(key)] == key {
			if len(key) > len(bestKey) {
				bestKey = key
			}
		}
	}
	if bestKey != "" {
		return PricingTable[bestKey]
	}
	return PricingTable["default"]
}

// EstimateCost calculates estimated cost in USD given token counts and model.
func EstimateCost(model string, input, output, cacheCreate, cacheRead int) float64 {
	p := LookupPricing(model)
	return float64(input)*p.InputPerM/1_000_000 +
		float64(output)*p.OutputPerM/1_000_000 +
		float64(cacheCreate)*p.CacheWritePerM/1_000_000 +
		float64(cacheRead)*p.CacheReadPerM/1_000_000
}
