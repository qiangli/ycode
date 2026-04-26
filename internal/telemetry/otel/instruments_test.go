package otel

import (
	"testing"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestNewInstruments(t *testing.T) {
	m := noop.NewMeterProvider().Meter("test")
	inst, err := NewInstruments(m)
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}

	// Verify all instruments are non-nil.
	checks := []struct {
		name string
		ok   bool
	}{
		// LLM
		{"LLMCallDuration", inst.LLMCallDuration != nil},
		{"LLMCallTotal", inst.LLMCallTotal != nil},
		{"LLMTokensInput", inst.LLMTokensInput != nil},
		{"LLMTokensOutput", inst.LLMTokensOutput != nil},
		{"LLMTokensCacheRead", inst.LLMTokensCacheRead != nil},
		{"LLMTokensCacheWrite", inst.LLMTokensCacheWrite != nil},
		{"LLMCostDollars", inst.LLMCostDollars != nil},
		{"LLMContextUsed", inst.LLMContextUsed != nil},
		// Tool
		{"ToolCallDuration", inst.ToolCallDuration != nil},
		{"ToolCallTotal", inst.ToolCallTotal != nil},
		// Turn
		{"TurnDuration", inst.TurnDuration != nil},
		{"TurnToolCount", inst.TurnToolCount != nil},
		{"SessionTurns", inst.SessionTurns != nil},
		// Session
		{"SessionDuration", inst.SessionDuration != nil},
		{"SessionTotalCost", inst.SessionTotalCost != nil},
		{"SessionTokensIn", inst.SessionTokensIn != nil},
		{"SessionTokensOut", inst.SessionTokensOut != nil},
		// Turn file changes
		{"TurnFilesChanged", inst.TurnFilesChanged != nil},
		{"TurnLinesAdded", inst.TurnLinesAdded != nil},
		{"TurnLinesDeleted", inst.TurnLinesDeleted != nil},
		// Compaction
		{"CompactionTotal", inst.CompactionTotal != nil},
		{"CompactionTokensSaved", inst.CompactionTokensSaved != nil},
		// Pause/resume
		{"PauseTotal", inst.PauseTotal != nil},
		{"PauseDuration", inst.PauseDuration != nil},
		{"ResumeTotal", inst.ResumeTotal != nil},
		// API errors
		{"APIErrorTotal", inst.APIErrorTotal != nil},
		{"MessageStructureWarnings", inst.MessageStructureWarnings != nil},
		// General error
		{"ErrorTotal", inst.ErrorTotal != nil},
		// Inference
		{"InferenceCallDuration", inst.InferenceCallDuration != nil},
		{"InferenceCallTotal", inst.InferenceCallTotal != nil},
		{"InferenceTokensInput", inst.InferenceTokensInput != nil},
		{"InferenceTokensOutput", inst.InferenceTokensOutput != nil},
		{"InferenceModelLoadTime", inst.InferenceModelLoadTime != nil},
		{"InferenceRunnerStarts", inst.InferenceRunnerStarts != nil},
		{"InferenceRunnerCrashes", inst.InferenceRunnerCrashes != nil},
	}

	for _, c := range checks {
		if !c.ok {
			t.Errorf("instrument %s is nil", c.name)
		}
	}
}
