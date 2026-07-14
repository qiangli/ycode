package usage

import "testing"

// A NUMBER THAT LOOKS LIKE A FACT AND IS NOT ONE IS WORSE THAN A MISSING NUMBER.
//
// The pricing fallback is $3/$15 per million — Claude Sonnet's rate. So a model the table
// had never heard of was not reported as "cost unknown". It was reported as a confident,
// specific, WRONG number.
//
// GLM-5.2 was exactly that model. Its real API price is ~$0.60/M input, and on the flat-
// rate coding plan its MARGINAL cost is zero. The fallback billed it at 5x its API price,
// like a frontier Anthropic model. A cost dashboard would have shown the cheapest model in
// the fleet as the most expensive one — and every routing decision that read that number
// would have been backwards.
func TestTheFleetsModelsHaveRealPrices(t *testing.T) {
	// Every model bashy can actually route to. A missing price here is not a gap in a
	// table; it is a wrong number in a dashboard.
	for _, model := range []string{
		"glm-5.2", "glm-4.6",
		"kimi-k2.7-code", "moonshot-v1-128k",
		"deepseek-v4-pro", "deepseek-chat",
		"claude-opus-4-8", "claude-sonnet-4-6",
		"gpt-4.1", "o3",
		"gemini-2.5-pro",
		"qwen-max", "grok-3",
	} {
		p := LookupPricing(model)
		if !p.Known {
			t.Errorf("%s has NO declared price — it is silently billed at the fallback rate "+
				"($%.2f/$%.2f per M, which is Claude Sonnet's), and that number will be "+
				"reported as fact", model, PricingTable["default"].InputPerM, PricingTable["default"].OutputPerM)
		}
	}
}

// The specific lie, pinned: GLM must not be billed like Claude.
func TestGLMIsNotBilledAtClaudeRates(t *testing.T) {
	glm := LookupPricing("glm-5.2")
	fallback := PricingTable["default"]

	if !glm.Known {
		t.Fatal("glm-5.2 has no declared price")
	}
	if glm.InputPerM >= fallback.InputPerM {
		t.Errorf("glm-5.2 input priced at $%.2f/M — at or above the Claude-rate fallback "+
			"($%.2f/M). It is one of the CHEAPEST models in the fleet, not one of the "+
			"dearest.", glm.InputPerM, fallback.InputPerM)
	}

	// The number that matters: a real turn's cost, against what the fallback would have said.
	const in, out = 100_000, 10_000
	real := EstimateCost("glm-5.2", in, out, 0, 0)
	lie := float64(in)*fallback.InputPerM/1e6 + float64(out)*fallback.OutputPerM/1e6

	if real >= lie {
		t.Errorf("a 100k/10k turn: glm reports $%.4f, the fallback would have said $%.4f — "+
			"the fix changed nothing", real, lie)
	}
	t.Logf("100k in / 10k out — glm-5.2: $%.4f   (the old fallback would have claimed $%.4f, %.1fx)",
		real, lie, lie/real)
}

// An unknown model must SAY it is unknown. That flag is the whole point: without it,
// nobody downstream can tell a measured cost from a guessed one.
func TestAnUnknownModelAdmitsItIsGuessing(t *testing.T) {
	p := LookupPricing("some-model-nobody-has-heard-of-v9")
	if p.Known {
		t.Error("a model absent from the table reported Known=true — a guess is masquerading " +
			"as a measurement")
	}
	if p.InputPerM == 0 {
		t.Error("this test has gone stale: the fallback is now zero, so the failure mode is " +
			"'silently free' rather than 'silently expensive'. Both are wrong; update the note.")
	}
}
