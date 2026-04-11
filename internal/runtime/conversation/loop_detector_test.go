package conversation

import (
	"testing"
)

func TestLoopDetector_NoLoop(t *testing.T) {
	d := NewLoopDetector()

	if s := d.Record("I fixed the authentication bug in login.go"); s != LoopNone {
		t.Errorf("expected none, got %s", s)
	}
	if s := d.Record("Now let me update the test file for the new behavior"); s != LoopNone {
		t.Errorf("expected none, got %s", s)
	}
	if s := d.Record("All tests pass. The migration is complete."); s != LoopNone {
		t.Errorf("expected none, got %s", s)
	}
}

func TestLoopDetector_SoftThreshold(t *testing.T) {
	d := NewLoopDetector()

	for range LoopSoftThreshold - 1 {
		s := d.Record("same response")
		if s != LoopNone {
			t.Fatalf("expected none before threshold, got %s", s)
		}
	}

	s := d.Record("same response")
	if s != LoopWarning {
		t.Errorf("expected warning at soft threshold, got %s", s)
	}
}

func TestLoopDetector_HardThreshold(t *testing.T) {
	d := NewLoopDetector()

	for i := range LoopHardThreshold - 1 {
		s := d.Record("identical output")
		if s == LoopBreak {
			t.Fatalf("got break too early at iteration %d", i)
		}
	}

	s := d.Record("identical output")
	if s != LoopBreak {
		t.Errorf("expected break at hard threshold, got %s", s)
	}
}

func TestLoopDetector_SimilarButNotIdentical(t *testing.T) {
	d := NewLoopDetector()

	// Long responses that are ~90% identical (differ only at the end).
	base := "I'll help you fix that bug. Let me read the file src/main.go and check the authentication logic. The issue appears to be in the token validation handler where the expiry check uses the wrong time zone."

	for range LoopSoftThreshold - 1 {
		d.Record(base)
	}

	// ~90% identical — only last few words differ.
	similar := "I'll help you fix that bug. Let me read the file src/main.go and check the authentication logic. The issue appears to be in the token validation handler where the expiry check uses the wrong format."
	s := d.Record(similar)
	if s != LoopWarning {
		t.Errorf("expected warning for similar responses, got %s", s)
	}
}

func TestLoopDetector_DifferentResponsesBreakChain(t *testing.T) {
	d := NewLoopDetector()

	d.Record("response A")
	d.Record("response A")
	d.Record("totally different response here")
	d.Record("response A")
	d.Record("response A")

	// Only 2 consecutive similar at the end, should not trigger.
	s := d.Record("response A")
	if s != LoopWarning {
		t.Errorf("expected warning (3 consecutive), got %s", s)
	}
}

func TestLoopDetector_Reset(t *testing.T) {
	d := NewLoopDetector()

	for range LoopSoftThreshold {
		d.Record("same")
	}

	d.Reset()

	s := d.Record("same")
	if s != LoopNone {
		t.Errorf("expected none after reset, got %s", s)
	}
}

func TestIsSimilar(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected bool
	}{
		{"identical", "hello world", "hello world", true},
		{"both empty", "", "", false},
		{"one empty", "hello", "", false},
		{"very different", "hello world", "goodbye moon", false},
		{"slight diff", "I will fix the bug in main.go", "I will fix the bug in main.ts", true},
		{"length mismatch", "short", "this is a much longer string that is very different", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSimilar(tt.a, tt.b); got != tt.expected {
				t.Errorf("isSimilar(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}
