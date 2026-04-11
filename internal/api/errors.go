package api

import (
	"errors"
	"strings"
)

// TokenLimitError represents an API error due to exceeding model token limits.
type TokenLimitError struct {
	RequestedTokens int
	MaxTokens       int
	Message         string
}

func (e *TokenLimitError) Error() string {
	return e.Message
}

// IsTokenLimitError checks if an error is a token limit error.
func IsTokenLimitError(err error) bool {
	var tlErr *TokenLimitError
	return errors.As(err, &tlErr)
}

// ParseTokenLimitError attempts to parse a token limit error from API response.
// It handles various provider error message formats.
func ParseTokenLimitError(body string) *TokenLimitError {
	bodyLower := strings.ToLower(body)

	// Check for token limit related keywords
	hasTokenLimit := strings.Contains(bodyLower, "token limit") ||
		(strings.Contains(bodyLower, "exceeded") && strings.Contains(bodyLower, "token")) ||
		strings.Contains(bodyLower, "context length") ||
		strings.Contains(bodyLower, "maximum context") ||
		strings.Contains(bodyLower, "too large") ||
		strings.Contains(bodyLower, "too long") ||
		strings.Contains(bodyLower, "context window")

	if !hasTokenLimit {
		return nil
	}

	err := &TokenLimitError{
		Message: body,
	}

	// Try to extract token numbers from common patterns:
	// "exceeded model token limit: 262144 (requested: 876782)"
	// "maximum context length: 128000, requested: 150000"
	// "This model's maximum context length is 128000 tokens..."
	// "requested 150000, maximum 128000 tokens"

	// Pattern 1: "limit: X (requested: Y)"
	if idx := strings.Index(bodyLower, "limit:"); idx != -1 {
		rest := body[idx+6:]
		if maxTokens, ok := extractNumber(rest); ok {
			err.MaxTokens = maxTokens
		}
	}

	// Pattern 2: "requested: X" or "requested X"
	if idx := strings.Index(bodyLower, "requested"); idx != -1 {
		rest := body[idx+9:]
		// Skip optional colon or space
		rest = strings.TrimLeft(rest, ": ")
		if requested, ok := extractNumber(rest); ok {
			err.RequestedTokens = requested
		}
	}

	// Pattern 3: "maximum context length is X" or "maximum is X"
	if idx := strings.Index(bodyLower, "maximum context length is"); idx != -1 {
		rest := body[idx+25:]
		if maxTokens, ok := extractNumber(rest); ok {
			err.MaxTokens = maxTokens
		}
	} else if idx := strings.Index(bodyLower, "maximum is"); idx != -1 {
		rest := body[idx+10:]
		if maxTokens, ok := extractNumber(rest); ok {
			err.MaxTokens = maxTokens
		}
	}

	// Pattern 4: Look for numbers before "tokens" word
	if err.MaxTokens == 0 || err.RequestedTokens == 0 {
		tokensIdx := strings.Index(bodyLower, "tokens")
		if tokensIdx > 0 {
			// Search for numbers in the text
			var numbers []int
			for i := 0; i < len(body); {
				if body[i] >= '0' && body[i] <= '9' {
					if num, ok := extractNumber(body[i:]); ok {
						numbers = append(numbers, num)
						// Skip past this number
						for i < len(body) && body[i] >= '0' && body[i] <= '9' {
							i++
						}
						continue
					}
				}
				i++
			}
			// If we found exactly 2 numbers, assume first is max, second is requested
			// This handles "requested X, maximum Y" patterns
			if len(numbers) == 2 {
				if err.MaxTokens == 0 {
					err.MaxTokens = numbers[0]
				}
				if err.RequestedTokens == 0 {
					err.RequestedTokens = numbers[1]
				}
			} else if len(numbers) >= 2 {
				// For more numbers, use the last two (most likely to be the relevant ones)
				if err.RequestedTokens == 0 {
					err.RequestedTokens = numbers[len(numbers)-1]
				}
				if err.MaxTokens == 0 {
					err.MaxTokens = numbers[len(numbers)-2]
				}
			}
		}
	}

	return err
}

// extractNumber extracts the first integer from a string.
func extractNumber(s string) (int, bool) {
	// Skip non-digit characters
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		i++
	}
	if i >= len(s) {
		return 0, false
	}

	// Parse the number
	num := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		num = num*10 + int(s[i]-'0')
		i++
	}

	return num, num > 0
}
