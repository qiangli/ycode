package autoloop

import "testing"

func TestDetectQuestions_None(t *testing.T) {
	response := "I'll implement the changes now. First, let me read the file."
	count := DetectQuestions(response)
	if count != 0 {
		t.Errorf("question count = %d, want 0", count)
	}
}

func TestDetectQuestions_Multiple(t *testing.T) {
	response := "Should I use the existing interface? Would you like me to add tests too? Can you clarify the scope?"
	count := DetectQuestions(response)
	if count < 3 {
		t.Errorf("question count = %d, want >= 3", count)
	}
}

func TestIsAskingQuestions(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     bool
	}{
		{
			name:     "no questions",
			response: "Implementing the feature now.",
			want:     false,
		},
		{
			name:     "one question (below threshold)",
			response: "Should I proceed with this approach?",
			want:     false,
		},
		{
			name:     "two questions (meets threshold)",
			response: "Should I add tests? Do you want me to refactor too?",
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAskingQuestions(tt.response)
			if got != tt.want {
				t.Errorf("IsAskingQuestions = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectCompletionIndicators(t *testing.T) {
	tests := []struct {
		response string
		wantMin  int
	}{
		{"Still working on tests.", 0},
		{"All tasks completed. Work is done.", 2},
		{"Successfully implemented the feature. No remaining gaps.", 2},
	}
	for _, tt := range tests {
		count := DetectCompletionIndicators(tt.response)
		if count < tt.wantMin {
			t.Errorf("response %q: indicators = %d, want >= %d", tt.response[:20], count, tt.wantMin)
		}
	}
}

func TestParseExitSignal(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     ExitSignal
	}{
		{
			name:     "no signal",
			response: "Just some text without any signal.",
			want:     ExitSignal{},
		},
		{
			name:     "exit true",
			response: "Summary of work.\nEXIT_SIGNAL: true\nWORK_TYPE: IMPLEMENTATION",
			want:     ExitSignal{Detected: true, WorkType: "IMPLEMENTATION"},
		},
		{
			name:     "exit false",
			response: "EXIT_SIGNAL: false\nWORK_TYPE: TESTING",
			want:     ExitSignal{Detected: false, WorkType: "TESTING"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseExitSignal(tt.response)
			if got.Detected != tt.want.Detected {
				t.Errorf("Detected = %v, want %v", got.Detected, tt.want.Detected)
			}
			if got.WorkType != tt.want.WorkType {
				t.Errorf("WorkType = %q, want %q", got.WorkType, tt.want.WorkType)
			}
		})
	}
}

func TestShouldExit_RequiresBothLayers(t *testing.T) {
	// Only indicators — not enough.
	response1 := "All tasks completed. Work is done. No remaining tasks."
	if ShouldExit(response1, 2) {
		t.Error("should not exit with only indicators (no EXIT_SIGNAL)")
	}

	// Only EXIT_SIGNAL — not enough.
	response2 := "Still working.\nEXIT_SIGNAL: true"
	if ShouldExit(response2, 2) {
		t.Error("should not exit with only EXIT_SIGNAL (no indicators)")
	}

	// Both — should exit.
	response3 := "All tasks completed. Work is done.\nEXIT_SIGNAL: true\nWORK_TYPE: IMPLEMENTATION"
	if !ShouldExit(response3, 2) {
		t.Error("should exit when both indicators and EXIT_SIGNAL present")
	}
}

func TestQuestionSuppressionMessage(t *testing.T) {
	msg := QuestionSuppressionMessage()
	if msg == "" {
		t.Error("expected non-empty suppression message")
	}
}
