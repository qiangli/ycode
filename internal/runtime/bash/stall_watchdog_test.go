package bash

import (
	"testing"
)

func TestDetectInteractivePrompt(t *testing.T) {
	tests := []struct {
		tail    string
		matched bool
	}{
		{"normal output\nmore output\n", false},
		{"Do you want to continue? [y/N] ", true},
		{"Enter password: Password:", true},
		{"Are you sure you want to proceed?", true},
		{"Press Enter to continue", true},
		{"Overwrite existing file? [Y/n]", true},
		{"Building project...\nDone.", false},
	}

	for _, tt := range tests {
		result := DetectInteractivePrompt(tt.tail)
		if (result != "") != tt.matched {
			t.Errorf("DetectInteractivePrompt(%q) = %q, matched=%v want matched=%v",
				tt.tail, result, result != "", tt.matched)
		}
	}
}

func TestStallWatchdog_OnStall(t *testing.T) {
	sw := NewStallWatchdog()

	var received []StallEvent
	sw.OnStall(func(e StallEvent) {
		received = append(received, e)
	})

	// Simulate a stall event.
	sw.emit(StallEvent{
		TaskID:        "test-task",
		MatchedPrompt: "[y/N]",
		OutputTail:    "Continue? [y/N]",
	})

	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].TaskID != "test-task" {
		t.Errorf("expected task-id 'test-task', got %s", received[0].TaskID)
	}
}
