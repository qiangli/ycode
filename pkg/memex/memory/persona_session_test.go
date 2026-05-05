package memory

import (
	"testing"
	"time"
)

func TestUpdatePersonaFromSession(t *testing.T) {
	env := &EnvironmentSignals{Platform: "darwin", GitUserName: "alice"}
	p := NewPersona("test-id", env)

	// Simulate a debugging session with terse messages.
	for i := range 10 {
		p.SessionContext.Update(SessionSignal{
			TurnNumber:       i,
			MessageLength:    5, // short messages
			QuestionCount:    0,
			TechnicalDensity: 0.5,
			ToolApprovals:    1,
			Corrections:      0,
			DetectedIntent:   "debugging",
			Timestamp:        time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	UpdatePersonaFromSession(p)

	// Verbosity should drift toward 0 (short messages).
	if p.Communication.Verbosity >= 0.5 {
		t.Errorf("Verbosity = %.2f, want < 0.5 for short messages", p.Communication.Verbosity)
	}

	// After one session with EMA alpha=0.2, verbosity drifts from 0.5 to 0.4.
	// JustDoIt requires verbosity < 0.3, so it won't trigger after just one session.
	// After multiple sessions it would converge. Verify the drift direction instead.
	if p.Communication.Verbosity >= 0.5 {
		t.Error("Verbosity should have decreased from 0.5")
	}

	// Tool approval rate should drift toward 1.0.
	if p.Behavior.ToolApprovalRate <= 0.5 {
		t.Errorf("ToolApprovalRate = %.2f, want > 0.5", p.Behavior.ToolApprovalRate)
	}

	// Interaction summary should be updated.
	if p.Interactions.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", p.Interactions.TotalSessions)
	}
	if p.Interactions.TotalTurns != 10 {
		t.Errorf("TotalTurns = %d, want 10", p.Interactions.TotalTurns)
	}

	// Communication confidence should have increased.
	if p.Communication.Confidence < 0.05 {
		t.Errorf("Communication.Confidence = %.2f, want >= 0.05", p.Communication.Confidence)
	}
}

func TestUpdatePersonaFromSession_Nil(t *testing.T) {
	// Should not panic.
	UpdatePersonaFromSession(nil)
	UpdatePersonaFromSession(&Persona{})
}

func TestUpdatePersonaFromSession_HighCorrections(t *testing.T) {
	env := &EnvironmentSignals{Platform: "darwin"}
	p := NewPersona("test-id", env)

	// Session with many corrections.
	for i := range 6 {
		p.SessionContext.Update(SessionSignal{
			TurnNumber:     i,
			MessageLength:  20,
			Corrections:    2,
			DetectedIntent: "debugging",
			Timestamp:      time.Now().Add(time.Duration(i) * time.Minute),
		})
	}

	UpdatePersonaFromSession(p)

	// Correction frequency should be high.
	if p.Behavior.CorrectionFreq <= 0.5 {
		t.Errorf("CorrectionFreq = %.2f, want > 0.5 for high-correction session", p.Behavior.CorrectionFreq)
	}
}

func TestEMA(t *testing.T) {
	// Starting at 0.5, new value 1.0, alpha 0.2 → 0.6.
	result := ema(0.5, 1.0, 0.2)
	if result < 0.59 || result > 0.61 {
		t.Errorf("ema(0.5, 1.0, 0.2) = %.4f, want ~0.6", result)
	}
}

func TestClamp01(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{-0.5, 0},
		{0.5, 0.5},
		{1.5, 1.0},
	}
	for _, tt := range tests {
		got := clamp01(tt.input)
		if got != tt.want {
			t.Errorf("clamp01(%f) = %f, want %f", tt.input, got, tt.want)
		}
	}
}
