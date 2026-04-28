package agentsmd

// Scoring weights for the composite quality score.
const (
	weightCommandDensity   = 0.25
	weightGuardrailDensity = 0.25
	weightNoBoilerplate    = 0.20
	weightPathAccuracy     = 0.15
	weightCommandAccuracy  = 0.15
)

// ComputeScore computes a composite quality score from individual metrics.
// Returns a value in [0.0, 1.0]. Higher is better.
func ComputeScore(r *Report) float64 {
	// Command density: normalize to [0,1]. ~5% code blocks is good, cap at 10%.
	cmdScore := clamp(r.CommandDensity/0.10, 0, 1)

	// Guardrail density: ~5% is good, cap at 10%.
	guardScore := clamp(r.GuardrailDensity/0.10, 0, 1)

	// Boilerplate: 0% is perfect, penalize linearly.
	boilerScore := clamp(1.0-r.BoilerplateRatio*10, 0, 1) // 10% boilerplate = 0 score

	// Path and command accuracy are already [0,1].
	pathScore := r.PathAccuracy
	cmdAccScore := r.CommandAccuracy

	return weightCommandDensity*cmdScore +
		weightGuardrailDensity*guardScore +
		weightNoBoilerplate*boilerScore +
		weightPathAccuracy*pathScore +
		weightCommandAccuracy*cmdAccScore
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
