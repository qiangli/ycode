package recovery

import (
	"fmt"
	"strings"
)

// RecipeKind identifies the type of recovery recipe.
type RecipeKind string

const (
	RecipeRetry       RecipeKind = "retry"
	RecipeRollback    RecipeKind = "rollback"
	RecipeSkip        RecipeKind = "skip"
	RecipeEscalate    RecipeKind = "escalate"
	RecipeAlternative RecipeKind = "alternative"
)

// Recipe describes a recovery action for a specific failure.
type Recipe struct {
	Kind        RecipeKind `json:"kind"`
	Description string     `json:"description"`
	MaxAttempts int        `json:"max_attempts,omitempty"`
	Action      string     `json:"action,omitempty"`
}

// Attempt tracks a recovery attempt.
type Attempt struct {
	Recipe    *Recipe `json:"recipe"`
	Iteration int     `json:"iteration"`
	Succeeded bool    `json:"succeeded"`
	Error     string  `json:"error,omitempty"`
}

// Result is the outcome of a recovery attempt.
type Result struct {
	Recovered bool      `json:"recovered"`
	Attempts  []Attempt `json:"attempts"`
	FinalErr  string    `json:"final_error,omitempty"`
}

// AttemptRecovery tries to recover from an error using matching recipes.
func AttemptRecovery(err error, recipes []Recipe) *Result {
	result := &Result{}
	errMsg := err.Error()

	for _, recipe := range recipes {
		if !matchesError(recipe, errMsg) {
			continue
		}

		maxAttempts := recipe.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 1
		}

		for i := 0; i < maxAttempts; i++ {
			attempt := Attempt{
				Recipe:    &recipe,
				Iteration: i + 1,
			}

			// Execute recovery based on kind.
			switch recipe.Kind {
			case RecipeRetry:
				// Caller should retry the operation.
				attempt.Succeeded = true
				result.Recovered = true
			case RecipeSkip:
				attempt.Succeeded = true
				result.Recovered = true
			case RecipeEscalate:
				attempt.Succeeded = false
				attempt.Error = "escalated to user"
			case RecipeRollback:
				attempt.Succeeded = true
				result.Recovered = true
			case RecipeAlternative:
				attempt.Succeeded = true
				result.Recovered = true
			}

			result.Attempts = append(result.Attempts, attempt)
			if attempt.Succeeded {
				return result
			}
		}
	}

	if !result.Recovered {
		result.FinalErr = errMsg
	}
	return result
}

// matchesError checks if a recipe applies to the given error.
func matchesError(recipe Recipe, errMsg string) bool {
	// Match on recipe description keywords.
	lower := strings.ToLower(errMsg)
	desc := strings.ToLower(recipe.Description)

	// Simple keyword overlap matching.
	for _, word := range strings.Fields(desc) {
		if len(word) > 3 && strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// DefaultRecipes returns built-in recovery recipes.
func DefaultRecipes() []Recipe {
	return []Recipe{
		{Kind: RecipeRetry, Description: "rate limit exceeded", MaxAttempts: 3},
		{Kind: RecipeRetry, Description: "connection timeout", MaxAttempts: 2},
		{Kind: RecipeRetry, Description: "temporary failure", MaxAttempts: 2},
		{Kind: RecipeSkip, Description: "file not found", MaxAttempts: 1},
		{Kind: RecipeEscalate, Description: "permission denied", MaxAttempts: 1},
		{Kind: RecipeAlternative, Description: "command not found", MaxAttempts: 1},
	}
}

// FormatResult formats a recovery result for display.
func FormatResult(r *Result) string {
	if r.Recovered {
		return fmt.Sprintf("Recovered after %d attempt(s)", len(r.Attempts))
	}
	return fmt.Sprintf("Recovery failed after %d attempt(s): %s", len(r.Attempts), r.FinalErr)
}
