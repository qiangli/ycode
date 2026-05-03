package tools

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

// === Phase 1A: Error Classification Tests ===

func TestClassifyError_Fatal(t *testing.T) {
	tests := []struct {
		msg  string
		want ErrorCategory
	}{
		{"permission denied", ErrorFatal},
		{"access denied for /etc/shadow", ErrorFatal},
		{"command not found: foobar", ErrorFatal},
	}
	for _, tt := range tests {
		te := ClassifyError("bash", errors.New(tt.msg))
		if te.Category != tt.want {
			t.Errorf("ClassifyError(%q) = %s, want %s", tt.msg, te.Category, tt.want)
		}
		if te.Retryable {
			t.Errorf("fatal error should not be retryable: %q", tt.msg)
		}
	}
}

func TestClassifyError_Transient(t *testing.T) {
	tests := []string{
		"connection refused",
		"i/o timeout",
		"rate limit exceeded",
		"503 service temporarily unavailable",
	}
	for _, msg := range tests {
		te := ClassifyError("WebFetch", errors.New(msg))
		if te.Category != ErrorTransient {
			t.Errorf("ClassifyError(%q) = %s, want transient", msg, te.Category)
		}
		if !te.Retryable {
			t.Errorf("transient error should be retryable: %q", msg)
		}
	}
}

func TestClassifyError_Recoverable(t *testing.T) {
	te := ClassifyError("edit_file", fmt.Errorf("old_string not found in /path/to/file.go"))
	if te.Category != ErrorRecoverable {
		t.Errorf("expected recoverable, got %s", te.Category)
	}
	if te.RecoveryHint == "" {
		t.Error("expected recovery hint for 'not found' error")
	}
}

func TestClassifyError_Validation(t *testing.T) {
	te := ClassifyError("edit_file", fmt.Errorf("old_string and new_string are identical"))
	if te.Category != ErrorValidation {
		t.Errorf("expected validation, got %s", te.Category)
	}
}

func TestClassifyError_MultipleOccurrences(t *testing.T) {
	te := ClassifyError("edit_file", fmt.Errorf("old_string appears 3 times in file.go"))
	if te.Category != ErrorRecoverable {
		t.Errorf("expected recoverable, got %s", te.Category)
	}
	if te.RecoveryHint == "" {
		t.Error("expected recovery hint for multiple occurrences")
	}
}

func TestClassifyError_Nil(t *testing.T) {
	te := ClassifyError("bash", nil)
	if te != nil {
		t.Error("expected nil for nil error")
	}
}

func TestToolError_ErrorInterface(t *testing.T) {
	te := &ToolError{
		Category:     ErrorRecoverable,
		ToolName:     "edit_file",
		Err:          fmt.Errorf("not found"),
		RecoveryHint: "try grep",
	}
	msg := te.Error()
	if msg == "" {
		t.Fatal("expected non-empty error message")
	}
	if te.Unwrap() == nil {
		t.Fatal("expected non-nil unwrapped error")
	}
}

// === Phase 1B: Guardrails & Mistake Tracking Tests ===

func TestToolMutability(t *testing.T) {
	if GetToolMutability("read_file") != ToolIdempotent {
		t.Error("read_file should be idempotent")
	}
	if GetToolMutability("edit_file") != ToolMutating {
		t.Error("edit_file should be mutating (default)")
	}
	if GetToolMutability("grep_search") != ToolIdempotent {
		t.Error("grep_search should be idempotent")
	}
	if GetToolMutability("bash") != ToolMutating {
		t.Error("bash should be mutating (default)")
	}
}

func TestMistakeTracker_Success(t *testing.T) {
	mt := NewMistakeTracker(DefaultGuardrailConfig())

	// Record some failures then a success.
	mt.RecordFailure("edit_file", "hash1", nil)
	mt.RecordFailure("edit_file", "hash1", nil)
	mt.RecordSuccess("edit_file")

	if mt.ConsecutiveFailures("edit_file") != 0 {
		t.Error("expected 0 consecutive failures after success")
	}
}

func TestMistakeTracker_ExactFailWarn(t *testing.T) {
	config := DefaultGuardrailConfig()
	config.ExactFailWarn = 2
	mt := NewMistakeTracker(config)

	r1 := mt.RecordFailure("edit_file", "same_hash", nil)
	if r1.Action != ActionNone {
		t.Error("first failure should not trigger action")
	}

	r2 := mt.RecordFailure("edit_file", "same_hash", nil)
	if r2.Action != ActionWarn {
		t.Errorf("second identical failure should warn, got %d", r2.Action)
	}
}

func TestMistakeTracker_ExactFailBlock(t *testing.T) {
	config := DefaultGuardrailConfig()
	config.ExactFailWarn = 2
	config.ExactFailBlock = 4
	mt := NewMistakeTracker(config)

	for i := 0; i < 4; i++ {
		mt.RecordFailure("edit_file", "same_hash", nil)
	}

	r := mt.RecordFailure("edit_file", "same_hash", nil)
	// 5th failure of same hash should be at or above block threshold (4)
	if r.Action != ActionBlock {
		t.Errorf("expected block after %d exact failures, got %d", config.ExactFailBlock, r.Action)
	}
}

func TestMistakeTracker_SameToolFailHalt(t *testing.T) {
	config := DefaultGuardrailConfig()
	config.SameToolFailHalt = 4
	mt := NewMistakeTracker(config)

	for i := 0; i < 4; i++ {
		mt.RecordFailure("edit_file", fmt.Sprintf("hash_%d", i), nil)
	}

	if mt.ConsecutiveFailures("edit_file") != 4 {
		t.Errorf("expected 4 consecutive failures, got %d", mt.ConsecutiveFailures("edit_file"))
	}
}

func TestMistakeTracker_NoProgress(t *testing.T) {
	config := DefaultGuardrailConfig()
	config.NoProgressWarn = 2
	config.NoProgressBlock = 4
	mt := NewMistakeTracker(config)

	r1 := mt.EndTurn(false)
	if r1.Action != ActionNone {
		t.Error("first no-progress turn should not trigger")
	}

	r2 := mt.EndTurn(false)
	if r2.Action != ActionWarn {
		t.Errorf("second no-progress turn should warn, got %d", r2.Action)
	}

	// Success resets.
	r3 := mt.EndTurn(true)
	if r3.Action != ActionNone {
		t.Error("success should reset no-progress counter")
	}
}

func TestMistakeTracker_Reset(t *testing.T) {
	mt := NewMistakeTracker(DefaultGuardrailConfig())
	mt.RecordFailure("bash", "h1", nil)
	mt.EndTurn(false)

	mt.Reset()

	if mt.ConsecutiveFailures("bash") != 0 {
		t.Error("expected 0 after reset")
	}
}

// === Phase 4: Lint Tests ===

func TestParseLintOutput_GoFormat(t *testing.T) {
	output := `main.go:10:5: undeclared name: foo
main.go:15:1: missing return at end of function
`
	errors := ParseLintOutput(output, ".go")
	if len(errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errors))
	}
	if errors[0].File != "main.go" {
		t.Errorf("expected main.go, got %s", errors[0].File)
	}
	if errors[0].Line != 10 {
		t.Errorf("expected line 10, got %d", errors[0].Line)
	}
	if errors[0].Column != 5 {
		t.Errorf("expected column 5, got %d", errors[0].Column)
	}
	if errors[0].Message != "undeclared name: foo" {
		t.Errorf("unexpected message: %s", errors[0].Message)
	}
}

func TestParseLintOutput_NoColumn(t *testing.T) {
	output := "file.py:42: syntax error\n"
	errors := ParseLintOutput(output, ".py")
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}
	if errors[0].Line != 42 {
		t.Errorf("expected line 42, got %d", errors[0].Line)
	}
}

func TestParseLintOutput_Empty(t *testing.T) {
	errors := ParseLintOutput("", ".go")
	if len(errors) != 0 {
		t.Errorf("expected 0 errors, got %d", len(errors))
	}
}

func TestFormatLintFeedback_NoErrors(t *testing.T) {
	result := FormatLintFeedback(nil, "")
	if result != "" {
		t.Error("expected empty string for no errors")
	}
}

func TestFormatLintFeedback_WithErrors(t *testing.T) {
	errors := []LintError{
		{File: "main.go", Line: 10, Column: 5, Message: "undeclared name"},
	}
	result := FormatLintFeedback(errors, "/nonexistent/path")
	if result == "" {
		t.Fatal("expected non-empty feedback")
	}
	if !containsSubstr(result, "LINT ERRORS") {
		t.Error("expected LINT ERRORS header")
	}
	if !containsSubstr(result, "undeclared name") {
		t.Error("expected error message in feedback")
	}
}

// === Phase 5: Approval Cache Tests ===

func TestApprovalCache_RecordAndCheck(t *testing.T) {
	cache := NewApprovalCache(10 * time.Minute)

	if cache.IsApproved("bash") {
		t.Error("expected not approved before record")
	}

	cache.Record("bash")

	if !cache.IsApproved("bash") {
		t.Error("expected approved after record")
	}

	if cache.IsApproved("edit_file") {
		t.Error("expected not approved for different tool")
	}
}

func TestApprovalCache_TTL(t *testing.T) {
	cache := NewApprovalCache(1 * time.Millisecond)

	cache.Record("bash")
	time.Sleep(5 * time.Millisecond)

	if cache.IsApproved("bash") {
		t.Error("expected expired after TTL")
	}
}

func TestApprovalCache_RecordMultiple(t *testing.T) {
	cache := NewApprovalCache(10 * time.Minute)
	cache.RecordMultiple([]string{"bash", "edit_file", "write_file"})

	if cache.Size() != 3 {
		t.Errorf("expected 3 cached, got %d", cache.Size())
	}
	for _, tool := range []string{"bash", "edit_file", "write_file"} {
		if !cache.IsApproved(tool) {
			t.Errorf("expected %s approved", tool)
		}
	}
}

func TestApprovalCache_Clear(t *testing.T) {
	cache := NewApprovalCache(10 * time.Minute)
	cache.Record("bash")
	cache.Clear()

	if cache.IsApproved("bash") {
		t.Error("expected not approved after clear")
	}
	if cache.Size() != 0 {
		t.Error("expected size 0 after clear")
	}
}

// helpers

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && indexOfSubstr(s, sub) >= 0
}

func indexOfSubstr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
