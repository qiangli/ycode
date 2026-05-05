package memory

import (
	"testing"
)

func TestObserveTurn_BasicSignals(t *testing.T) {
	sig := ObserveTurn("fix the nil pointer error in handler.go", nil, 1)

	if sig.TurnNumber != 1 {
		t.Errorf("TurnNumber = %d, want 1", sig.TurnNumber)
	}
	if sig.MessageLength != 7 {
		t.Errorf("MessageLength = %d, want 7", sig.MessageLength)
	}
	if sig.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestObserveTurn_QuestionDetection(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"fix this bug", 0},
		{"what is this error?", 2}, // "what" prefix + "?"
		{"how does the router work?", 2},
		{"can you explain this? and also this?", 3}, // "can" prefix + 2x "?"
		{"is this correct", 1},                      // "is" prefix only
	}

	for _, tt := range tests {
		sig := ObserveTurn(tt.msg, nil, 0)
		if sig.QuestionCount != tt.want {
			t.Errorf("QuestionCount(%q) = %d, want %d", tt.msg, sig.QuestionCount, tt.want)
		}
	}
}

func TestObserveTurn_TechnicalDensity(t *testing.T) {
	// Highly technical message.
	sig := ObserveTurn("add a goroutine with mutex and channel for the api handler", nil, 0)
	if sig.TechnicalDensity < 0.3 {
		t.Errorf("TechnicalDensity = %.2f, want >= 0.3 for technical message", sig.TechnicalDensity)
	}

	// Non-technical message.
	sig = ObserveTurn("please make the color blue and move it to the left", nil, 0)
	if sig.TechnicalDensity > 0.1 {
		t.Errorf("TechnicalDensity = %.2f, want <= 0.1 for non-technical message", sig.TechnicalDensity)
	}
}

func TestObserveTurn_CorrectionDetection(t *testing.T) {
	tests := []struct {
		msg  string
		want int
	}{
		{"looks good", 0},
		{"no, I meant the other file", 1},
		{"that's wrong, try again", 2}, // "that's wrong" + "try again"
		{"undo that please", 1},
	}

	for _, tt := range tests {
		sig := ObserveTurn(tt.msg, nil, 0)
		if sig.Corrections != tt.want {
			t.Errorf("Corrections(%q) = %d, want %d", tt.msg, sig.Corrections, tt.want)
		}
	}
}

func TestObserveTurn_IntentDetection(t *testing.T) {
	tests := []struct {
		msg    string
		intent string
	}{
		{"fix the error in the crash handler, it's failing", "debugging"},
		{"explain how the router works, help me understand", "learning"},
		{"should we refactor this to a different pattern", "architecting"},
		{"review the PR, the diff looks good", "reviewing"},
		{"hello world", ""},
	}

	for _, tt := range tests {
		sig := ObserveTurn(tt.msg, nil, 0)
		if sig.DetectedIntent != tt.intent {
			t.Errorf("DetectedIntent(%q) = %q, want %q", tt.msg, sig.DetectedIntent, tt.intent)
		}
	}
}

func TestObserveTurn_ToolOutcomes(t *testing.T) {
	outcomes := []ToolOutcome{
		{ToolName: "bash", Approved: true},
		{ToolName: "write", Approved: false},
		{ToolName: "edit", Approved: true, Corrected: true},
	}

	sig := ObserveTurn("run the tests", outcomes, 5)

	if sig.ToolApprovals != 2 {
		t.Errorf("ToolApprovals = %d, want 2", sig.ToolApprovals)
	}
	if sig.ToolDenials != 1 {
		t.Errorf("ToolDenials = %d, want 1", sig.ToolDenials)
	}
	// 1 correction from tool outcome (corrected: true)
	if sig.Corrections != 1 {
		t.Errorf("Corrections = %d, want 1", sig.Corrections)
	}
}

func TestObserveTurn_EmptyMessage(t *testing.T) {
	sig := ObserveTurn("", nil, 0)

	if sig.MessageLength != 0 {
		t.Errorf("MessageLength = %d, want 0", sig.MessageLength)
	}
	if sig.TechnicalDensity != 0 {
		t.Errorf("TechnicalDensity = %f, want 0", sig.TechnicalDensity)
	}
	if sig.DetectedIntent != "" {
		t.Errorf("DetectedIntent = %q, want empty", sig.DetectedIntent)
	}
}

func TestTechnicalDensity_Punctuation(t *testing.T) {
	// Technical terms with punctuation should still match.
	sig := ObserveTurn("check the api, then deploy.", nil, 0)
	if sig.TechnicalDensity == 0 {
		t.Error("TechnicalDensity should be > 0 for message with technical terms plus punctuation")
	}
}
