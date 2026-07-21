package observe

import "github.com/qiangli/ycode/internal/runtime/usage"

// CostFunc computes the USD cost of one model call from token counts. It is the
// "local price table" the design requires: tokens × a per-model price, computed
// entirely client-side (no billing API, no admin key, no gateway).
type CostFunc func(model string, promptTokens, completionTokens, cacheWrite, cacheRead int) float64

// DefaultCost delegates to ycode's canonical pricing table
// (internal/runtime/usage) so the action log and the rest of the runtime price
// a token identically — one source of truth, no drift.
func DefaultCost(model string, promptTokens, completionTokens, cacheWrite, cacheRead int) float64 {
	return usage.EstimateCost(model, promptTokens, completionTokens, cacheWrite, cacheRead)
}
