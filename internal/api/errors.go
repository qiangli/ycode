package api

import (
	"errors"
	"fmt"
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

// FailoverReason classifies API errors for smart recovery routing.
type FailoverReason int

const (
	ReasonUnknown         FailoverReason = iota
	ReasonAuth                           // transient auth error (invalid/expired key)
	ReasonAuthPermanent                  // permanent auth failure
	ReasonBilling                        // billing/payment issue
	ReasonRateLimit                      // 429 rate limited
	ReasonOverloaded                     // 529 or provider overloaded
	ReasonServerError                    // 5xx server errors
	ReasonTimeout                        // 408 or timeout in body
	ReasonContextOverflow                // token/context limit exceeded
	ReasonPayloadTooLarge                // request body too large (non-token)
	ReasonModelNotFound                  // requested model does not exist
	ReasonPolicyBlocked                  // content policy violation
	ReasonFormatError                    // malformed request
)

var failoverReasonNames = [...]string{
	"Unknown",
	"Auth",
	"AuthPermanent",
	"Billing",
	"RateLimit",
	"Overloaded",
	"ServerError",
	"Timeout",
	"ContextOverflow",
	"PayloadTooLarge",
	"ModelNotFound",
	"PolicyBlocked",
	"FormatError",
}

func (r FailoverReason) String() string {
	if int(r) < len(failoverReasonNames) {
		return failoverReasonNames[r]
	}
	return "Unknown"
}

// RecoveryAction indicates what to do for a given error.
type RecoveryAction int

const (
	ActionRetry           RecoveryAction = iota // retry with backoff
	ActionRotateKey                             // try a different API key
	ActionFallbackModel                         // fall back to an alternate model
	ActionCompressContext                       // compress/truncate context and retry
	ActionAbort                                 // non-recoverable, stop immediately
)

var recoveryActionNames = [...]string{
	"Retry",
	"RotateKey",
	"FallbackModel",
	"CompressContext",
	"Abort",
}

func (a RecoveryAction) String() string {
	if int(a) < len(recoveryActionNames) {
		return recoveryActionNames[a]
	}
	return "Retry"
}

// ClassifyError determines the FailoverReason from an HTTP status and response body.
func ClassifyError(statusCode int, body string) FailoverReason {
	bodyLower := strings.ToLower(body)

	switch {
	case (statusCode == 401 || statusCode == 403) &&
		(strings.Contains(bodyLower, "billing") || strings.Contains(bodyLower, "payment")):
		return ReasonBilling

	case (statusCode == 401 || statusCode == 403) &&
		(strings.Contains(bodyLower, "invalid") || strings.Contains(bodyLower, "expired")):
		return ReasonAuth

	case statusCode == 401 || statusCode == 403:
		// Generic auth error without specific keywords — still an auth issue.
		return ReasonAuth

	case statusCode == 429:
		return ReasonRateLimit

	case statusCode == 529:
		return ReasonOverloaded

	case statusCode == 500 || statusCode == 502 || statusCode == 503 || statusCode == 504:
		return ReasonServerError

	case statusCode == 408:
		return ReasonTimeout

	case (statusCode == 400 || statusCode == 413) && hasTokenLimitKeywords(bodyLower):
		return ReasonContextOverflow

	case statusCode == 404 && strings.Contains(bodyLower, "model"):
		return ReasonModelNotFound

	case statusCode == 400 &&
		(strings.Contains(bodyLower, "policy") || strings.Contains(bodyLower, "content")):
		return ReasonPolicyBlocked

	case strings.Contains(bodyLower, "overloaded"):
		return ReasonOverloaded

	case strings.Contains(bodyLower, "timeout"):
		return ReasonTimeout

	default:
		return ReasonUnknown
	}
}

// hasTokenLimitKeywords checks for token/context limit related keywords.
func hasTokenLimitKeywords(bodyLower string) bool {
	return strings.Contains(bodyLower, "token limit") ||
		strings.Contains(bodyLower, "context length") ||
		strings.Contains(bodyLower, "maximum context") ||
		strings.Contains(bodyLower, "too large") ||
		strings.Contains(bodyLower, "too long") ||
		strings.Contains(bodyLower, "context window") ||
		(strings.Contains(bodyLower, "exceeded") && strings.Contains(bodyLower, "token"))
}

// RecommendedAction returns the best recovery action for a given reason.
func (r FailoverReason) RecommendedAction() RecoveryAction {
	switch r {
	case ReasonAuth:
		return ActionRotateKey
	case ReasonAuthPermanent, ReasonBilling:
		return ActionAbort
	case ReasonRateLimit, ReasonOverloaded, ReasonServerError, ReasonTimeout:
		return ActionRetry
	case ReasonContextOverflow:
		return ActionCompressContext
	case ReasonModelNotFound:
		return ActionFallbackModel
	case ReasonPolicyBlocked, ReasonFormatError:
		return ActionAbort
	default:
		return ActionRetry
	}
}

// ClassifiedError wraps an API error with classification metadata.
type ClassifiedError struct {
	Reason     FailoverReason
	Action     RecoveryAction
	StatusCode int
	Body       string
}

func (e *ClassifiedError) Error() string {
	return fmt.Sprintf("API error %d [%s → %s]: %s",
		e.StatusCode, e.Reason, e.Action, truncateBody(e.Body, 200))
}

// truncateBody truncates a string for error display.
func truncateBody(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// IsClassifiedError checks if an error is a ClassifiedError.
func IsClassifiedError(err error) bool {
	var ce *ClassifiedError
	return errors.As(err, &ce)
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
