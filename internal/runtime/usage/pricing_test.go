package usage

import (
	"math"
	"testing"
)

func TestLookupPricing_ExactMatch(t *testing.T) {
	p := LookupPricing("claude-sonnet-4")
	if p.InputPerM != 3.0 {
		t.Errorf("expected InputPerM=3.0, got %f", p.InputPerM)
	}
	if p.OutputPerM != 15.0 {
		t.Errorf("expected OutputPerM=15.0, got %f", p.OutputPerM)
	}
}

func TestLookupPricing_PrefixMatch(t *testing.T) {
	p := LookupPricing("claude-sonnet-4-20250514")
	if p.InputPerM != 3.0 {
		t.Errorf("expected InputPerM=3.0 via prefix match, got %f", p.InputPerM)
	}
	if p.OutputPerM != 15.0 {
		t.Errorf("expected OutputPerM=15.0 via prefix match, got %f", p.OutputPerM)
	}
}

func TestLookupPricing_FallbackDefault(t *testing.T) {
	p := LookupPricing("unknown-model-xyz")
	def := PricingTable["default"]
	if p.InputPerM != def.InputPerM || p.OutputPerM != def.OutputPerM {
		t.Errorf("expected default pricing, got InputPerM=%f OutputPerM=%f", p.InputPerM, p.OutputPerM)
	}
}

func TestEstimateCost_Basic(t *testing.T) {
	// 1M input tokens + 1M output tokens on claude-sonnet-4 = $3 + $15 = $18
	cost := EstimateCost("claude-sonnet-4", 1_000_000, 1_000_000, 0, 0)
	if math.Abs(cost-18.0) > 0.001 {
		t.Errorf("expected cost ~18.0, got %f", cost)
	}
}

func TestEstimateCost_ZeroTokens(t *testing.T) {
	cost := EstimateCost("claude-sonnet-4", 0, 0, 0, 0)
	if cost != 0.0 {
		t.Errorf("expected cost 0.0 for zero tokens, got %f", cost)
	}
}
