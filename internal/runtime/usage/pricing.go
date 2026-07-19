package usage

// ModelPricing holds per-token costs in USD per million tokens.
type ModelPricing struct {
	InputPerM      float64 // cost per 1M input tokens
	OutputPerM     float64 // cost per 1M output tokens
	CacheWritePerM float64 // cost per 1M cache creation tokens
	CacheReadPerM  float64 // cost per 1M cache read tokens

	// Known is false when this pricing was NOT declared for the model — i.e. it is the
	// fallback guess.
	//
	// This exists because the fallback is not zero, it is $3/$15 per million — CLAUDE
	// SONNET's price. So an unknown model does not read as "cost unknown"; it reads as a
	// confident, specific, WRONG number. GLM-5.2's real API price is about $0.60/M, and
	// on a flat-rate coding plan its MARGINAL cost is zero — yet the fallback reported it
	// at 5x its API price and billed it like a frontier Anthropic model. A cost dashboard
	// would have shown the cheapest model in the fleet as the most expensive one.
	//
	// A number that looks like a fact and is not one is worse than a missing number.
	Known bool
}

// PricingTable maps model name patterns to pricing.
// Keys use prefix matching — "claude-sonnet" matches "claude-sonnet-4-20250514".
// PricingTable maps model name patterns to pricing.
// Keys use prefix matching — "claude-sonnet" matches "claude-sonnet-4-20250514".
// This is the single source of truth for model pricing. The telemetry/otel
// package delegates to this table via usage.EstimateCost().
var PricingTable = map[string]ModelPricing{
	// Anthropic Claude 4.x
	"claude-opus-4":   {InputPerM: 15.0, OutputPerM: 75.0, CacheWritePerM: 18.75, CacheReadPerM: 1.50, Known: true},
	"claude-sonnet-4": {InputPerM: 3.0, OutputPerM: 15.0, CacheWritePerM: 3.75, CacheReadPerM: 0.30, Known: true},
	"claude-haiku-4":  {InputPerM: 0.80, OutputPerM: 4.0, CacheWritePerM: 1.0, CacheReadPerM: 0.08, Known: true},
	// Anthropic Claude 3.5 (legacy)
	"claude-3-5-sonnet": {InputPerM: 3.0, OutputPerM: 15.0, CacheWritePerM: 3.75, CacheReadPerM: 0.30, Known: true},
	"claude-3-5-haiku":  {InputPerM: 0.80, OutputPerM: 4.0, CacheWritePerM: 1.0, CacheReadPerM: 0.08, Known: true},
	// OpenAI GPT-4o
	"gpt-4o":      {InputPerM: 2.50, OutputPerM: 10.0, CacheWritePerM: 0, CacheReadPerM: 1.25, Known: true},
	"gpt-4o-mini": {InputPerM: 0.15, OutputPerM: 0.60, CacheWritePerM: 0, CacheReadPerM: 0.075, Known: true},
	// OpenAI GPT-4.1
	"gpt-4.1":      {InputPerM: 2.0, OutputPerM: 8.0, Known: true},
	"gpt-4.1-mini": {InputPerM: 0.40, OutputPerM: 1.60, Known: true},
	"gpt-4.1-nano": {InputPerM: 0.10, OutputPerM: 0.40, Known: true},
	// OpenAI o-series
	"o3":      {InputPerM: 10.0, OutputPerM: 40.0, CacheWritePerM: 0, CacheReadPerM: 5.0, Known: true},
	"o3-mini": {InputPerM: 1.10, OutputPerM: 4.40, CacheWritePerM: 0, CacheReadPerM: 0.55, Known: true},
	"o4-mini": {InputPerM: 1.10, OutputPerM: 4.40, CacheWritePerM: 0, CacheReadPerM: 0.55, Known: true},
	// Google Gemini
	"gemini-2.5-pro":   {InputPerM: 1.25, OutputPerM: 10.0, CacheWritePerM: 0, CacheReadPerM: 0.315, Known: true},
	"gemini-2.5-flash": {InputPerM: 0.15, OutputPerM: 0.60, CacheWritePerM: 0, CacheReadPerM: 0.0375, Known: true},
	// DeepSeek (cache read = cache-hit input price). deepseek-chat /
	// deepseek-reasoner are deprecated (2026/07/24) and map to the
	// non-thinking / thinking modes of deepseek-v4-flash, sharing its price.
	"deepseek-chat":     {InputPerM: 0.14, OutputPerM: 0.28, CacheWritePerM: 0, CacheReadPerM: 0.0028, Known: true},
	"deepseek-reasoner": {InputPerM: 0.14, OutputPerM: 0.28, CacheWritePerM: 0, CacheReadPerM: 0.0028, Known: true},
	"deepseek-v4-flash": {InputPerM: 0.14, OutputPerM: 0.28, CacheWritePerM: 0, CacheReadPerM: 0.0028, Known: true},
	"deepseek-v4-pro":   {InputPerM: 0.435, OutputPerM: 0.87, CacheWritePerM: 0, CacheReadPerM: 0.003625, Known: true},
	// Local models (Ollama) — zero cost.
	// z.ai GLM. List API prices; note that on the GLM Coding Plan the MARGINAL cost is
	// zero (a prepaid seat), which is a property of the plan, not of the model — see the
	// billing split in coreutils/pkg/fleet.
	"glm-5.2": {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},
	"glm-5.1": {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},
	"glm-5":   {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},
	"glm-4.7": {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},
	"glm-4.6": {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},
	"glm-4.5": {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},
	"glm":     {InputPerM: 0.60, OutputPerM: 2.20, CacheReadPerM: 0.11, Known: true},

	// Moonshot / Kimi
	"kimi":     {InputPerM: 0.60, OutputPerM: 2.50, CacheReadPerM: 0.15, Known: true},
	"moonshot": {InputPerM: 0.60, OutputPerM: 2.50, CacheReadPerM: 0.15, Known: true},

	// Alibaba Qwen
	"qwen": {InputPerM: 0.40, OutputPerM: 1.20, Known: true},

	// xAI Grok
	"grok": {InputPerM: 3.00, OutputPerM: 15.00, Known: true},

	"local": {InputPerM: 0, OutputPerM: 0, Known: true},
	// Fallback — a GUESS, and it says so (Known: false).
	//
	// It is Claude Sonnet's price, which means an unknown model is silently billed at a
	// frontier Anthropic rate. That is not a conservative default; it is a specific,
	// confident, wrong number. Every caller that reports cost must carry Known through, so
	// "we do not know what this costs" never renders as a dollar figure that looks
	// authoritative.
	"default": {InputPerM: 3.0, OutputPerM: 15.0, CacheWritePerM: 3.75, CacheReadPerM: 0.30, Known: false},
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
