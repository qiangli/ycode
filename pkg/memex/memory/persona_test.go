package memory

import (
	"testing"
	"time"
)

func TestNewPersona(t *testing.T) {
	env := &EnvironmentSignals{
		Platform:    "darwin",
		Shell:       "zsh",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
		Hostname:    "macbook",
	}
	p := NewPersona("test-id", env)

	if p.ID != "test-id" {
		t.Errorf("ID = %q, want %q", p.ID, "test-id")
	}
	if p.DisplayHint != "alice" {
		t.Errorf("DisplayHint = %q, want %q", p.DisplayHint, "alice")
	}
	if p.Confidence != 0.5 {
		t.Errorf("Confidence = %f, want 0.5", p.Confidence)
	}
	if p.Communication.Verbosity != 0.5 {
		t.Errorf("Communication.Verbosity = %f, want 0.5", p.Communication.Verbosity)
	}
	if p.Behavior.PrefersTDD != 0.5 {
		t.Errorf("Behavior.PrefersTDD = %f, want 0.5", p.Behavior.PrefersTDD)
	}
	if p.Knowledge == nil {
		t.Error("Knowledge should not be nil")
	}
	if p.SessionContext == nil {
		t.Error("SessionContext should not be nil")
	}
}

func TestKnowledgeMap_AddOrUpdateDomain(t *testing.T) {
	km := &KnowledgeMap{}

	km.AddOrUpdateDomain("Go", LevelAdvanced, 0.8)
	if len(km.Domains) != 1 {
		t.Fatalf("Domains len = %d, want 1", len(km.Domains))
	}
	if km.Domains[0].Level != LevelAdvanced {
		t.Errorf("Level = %q, want %q", km.Domains[0].Level, LevelAdvanced)
	}
	if km.Domains[0].EvidenceCount != 1 {
		t.Errorf("EvidenceCount = %d, want 1", km.Domains[0].EvidenceCount)
	}

	// Update existing.
	km.AddOrUpdateDomain("Go", LevelExpert, 0.9)
	if len(km.Domains) != 1 {
		t.Fatalf("Domains len = %d, want 1 after update", len(km.Domains))
	}
	if km.Domains[0].Level != LevelExpert {
		t.Errorf("Level after update = %q, want %q", km.Domains[0].Level, LevelExpert)
	}
	if km.Domains[0].EvidenceCount != 2 {
		t.Errorf("EvidenceCount after update = %d, want 2", km.Domains[0].EvidenceCount)
	}
}

func TestKnowledgeMap_FindDomain(t *testing.T) {
	km := &KnowledgeMap{}
	km.AddOrUpdateDomain("Go", LevelAdvanced, 0.8)
	km.AddOrUpdateDomain("Python", LevelIntermediate, 0.6)

	d := km.FindDomain("Go")
	if d == nil {
		t.Fatal("FindDomain(Go) returned nil")
	}
	if d.Level != LevelAdvanced {
		t.Errorf("Level = %q, want %q", d.Level, LevelAdvanced)
	}

	if km.FindDomain("Rust") != nil {
		t.Error("FindDomain(Rust) should return nil")
	}
}

func TestInteractionSummary_AddObservation(t *testing.T) {
	is := &InteractionSummary{}

	// Fill up to MaxObservations.
	for i := 0; i < MaxObservations; i++ {
		is.AddObservation(PersonaObservation{
			Text:       "obs",
			Confidence: 0.5,
			ObservedAt: time.Now(),
		})
	}
	if len(is.Observations) != MaxObservations {
		t.Fatalf("len = %d, want %d", len(is.Observations), MaxObservations)
	}

	// Adding one more should evict lowest-confidence.
	is.Observations[3].Confidence = 0.1 // make one low confidence
	is.AddObservation(PersonaObservation{
		Text:       "new obs",
		Confidence: 0.9,
		ObservedAt: time.Now(),
	})
	if len(is.Observations) != MaxObservations {
		t.Fatalf("len = %d, want %d after eviction", len(is.Observations), MaxObservations)
	}
	// The evicted entry should have been replaced.
	if is.Observations[3].Text != "new obs" {
		t.Errorf("eviction target text = %q, want %q", is.Observations[3].Text, "new obs")
	}
}

func TestSessionContext_Update(t *testing.T) {
	sc := NewSessionContext()

	// Add signals and verify ring buffer behavior.
	for i := 0; i < signalHistorySize+5; i++ {
		sc.Update(SessionSignal{
			TurnNumber:     i,
			DetectedIntent: "debugging",
			Timestamp:      time.Now(),
		})
	}

	if len(sc.SignalHistory) != signalHistorySize {
		t.Errorf("SignalHistory len = %d, want %d", len(sc.SignalHistory), signalHistorySize)
	}
	// Oldest entries should be evicted.
	if sc.SignalHistory[0].TurnNumber != 5 {
		t.Errorf("oldest TurnNumber = %d, want 5", sc.SignalHistory[0].TurnNumber)
	}
}

func TestSessionContext_RoleDetection(t *testing.T) {
	sc := NewSessionContext()

	// Add 6 signals (triggers recompute at len%3==0): mostly debugging.
	for i := 0; i < 6; i++ {
		intent := "debugging"
		if i == 2 {
			intent = "learning"
		}
		sc.Update(SessionSignal{
			TurnNumber:     i,
			DetectedIntent: intent,
			Timestamp:      time.Now(),
		})
	}

	if sc.DetectedRole != "debugging" {
		t.Errorf("DetectedRole = %q, want %q", sc.DetectedRole, "debugging")
	}
}

func TestSessionContext_MoodDetection(t *testing.T) {
	sc := NewSessionContext()

	// High correction rate should trigger "frustrated".
	for i := 0; i < 6; i++ {
		sc.Update(SessionSignal{
			TurnNumber:     i,
			DetectedIntent: "debugging",
			Corrections:    1,
			Timestamp:      time.Now(),
		})
	}

	if sc.DetectedMood != "frustrated" {
		t.Errorf("DetectedMood = %q, want %q", sc.DetectedMood, "frustrated")
	}
}
