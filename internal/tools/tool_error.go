package tools

import (
	"fmt"
	"strings"
)

// ErrorCategory classifies tool call failures into actionable categories.
type ErrorCategory int

const (
	// ErrorFatal means abort immediately — the error is not recoverable by retrying.
	// Examples: "permission denied", "command not found".
	ErrorFatal ErrorCategory = iota

	// ErrorTransient means retry may succeed — the error is temporary.
	// Examples: network timeout, rate limit, server unavailable.
	ErrorTransient

	// ErrorValidation means the model sent bad parameters.
	// Examples: invalid JSON input, missing required field.
	ErrorValidation

	// ErrorRecoverable means the error is fixable with different approach.
	// Examples: "old_string not found" → use grep first.
	ErrorRecoverable
)

// String returns the category name.
func (c ErrorCategory) String() string {
	switch c {
	case ErrorFatal:
		return "fatal"
	case ErrorTransient:
		return "transient"
	case ErrorValidation:
		return "validation"
	case ErrorRecoverable:
		return "recoverable"
	default:
		return "unknown"
	}
}

// ToolError wraps a tool execution error with classification and recovery guidance.
type ToolError struct {
	Category     ErrorCategory
	ToolName     string
	Err          error
	RecoveryHint string // guidance text for the model
	Retryable    bool   // derived from category
}

// Error satisfies the error interface.
func (e *ToolError) Error() string {
	if e.RecoveryHint != "" {
		return fmt.Sprintf("%s [%s]: %v\nHint: %s", e.ToolName, e.Category, e.Err, e.RecoveryHint)
	}
	return fmt.Sprintf("%s [%s]: %v", e.ToolName, e.Category, e.Err)
}

// Unwrap returns the underlying error.
func (e *ToolError) Unwrap() error {
	return e.Err
}

// ClassifyError categorizes a tool error based on the error message.
func ClassifyError(toolName string, err error) *ToolError {
	if err == nil {
		return nil
	}

	msg := strings.ToLower(err.Error())
	te := &ToolError{ToolName: toolName, Err: err}

	switch {
	// Fatal errors — no point retrying.
	case contains(msg, "permission denied"),
		contains(msg, "access denied"),
		contains(msg, "not permitted"),
		contains(msg, "command not found"),
		contains(msg, "no such file or directory") && !contains(msg, "old_string"):
		te.Category = ErrorFatal
		te.Retryable = false

	// Transient errors — retry may help.
	case contains(msg, "timeout"),
		contains(msg, "timed out"),
		contains(msg, "connection refused"),
		contains(msg, "connection reset"),
		contains(msg, "i/o timeout"),
		contains(msg, "rate limit"),
		contains(msg, "too many requests"),
		contains(msg, "503"),
		contains(msg, "502"),
		contains(msg, "temporarily unavailable"):
		te.Category = ErrorTransient
		te.Retryable = true

	// Validation errors — model sent bad params.
	case contains(msg, "invalid"),
		contains(msg, "parse"),
		contains(msg, "unmarshal"),
		contains(msg, "missing required"),
		contains(msg, "old_string and new_string are identical"):
		te.Category = ErrorValidation
		te.Retryable = false
		te.RecoveryHint = "Check the tool parameters and try again with corrected values."

	// Recoverable errors — fixable with different approach.
	case contains(msg, "not found in"),
		contains(msg, "old_string not found"):
		te.Category = ErrorRecoverable
		te.Retryable = false
		te.RecoveryHint = "The exact text was not found. Use read_file to verify current content, or grep_search to find similar text."

	case contains(msg, "appears") && contains(msg, "times"):
		te.Category = ErrorRecoverable
		te.Retryable = false
		te.RecoveryHint = "The text appears multiple times. Provide more surrounding context to make it unique, or use replace_all."

	default:
		te.Category = ErrorRecoverable
		te.Retryable = true
	}

	return te
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
