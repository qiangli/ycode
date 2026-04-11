package otel

import (
	"math"
	"testing"
)

func TestLookupPricing(t *testing.T) {
	tests := []struct {
		model string
		want  float64 // input per million
	}{
		{"claude-sonnet-4-20250514", 3.0},
		{"claude-opus-4-6", 15.0},
		{"claude-haiku-4-5-20251001", 0.80},
		{"gpt-4o-2024-08-06", 2.50},
		{"gpt-4o-mini-2024-07-18", 0.15},
		{"unknown-model-xyz", 3.0}, // default
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := LookupPricing(tt.model)
			if p.InputPerMillion != tt.want {
				t.Errorf("LookupPricing(%q).InputPerMillion = %v, want %v", tt.model, p.InputPerMillion, tt.want)
			}
		})
	}
}

func TestEstimateCost(t *testing.T) {
	cost := EstimateCost("claude-sonnet-4-20250514", 1000, 500, 200, 100)
	// input: 1000 * 3.0 / 1M = 0.003
	// output: 500 * 15.0 / 1M = 0.0075
	// cache write: 200 * 3.75 / 1M = 0.00075
	// cache read: 100 * 0.30 / 1M = 0.00003
	expected := 0.003 + 0.0075 + 0.00075 + 0.00003
	if math.Abs(cost-expected) > 1e-10 {
		t.Errorf("EstimateCost = %v, want %v", cost, expected)
	}
}
