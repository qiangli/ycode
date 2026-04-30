package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

func TestPersonaSection_Nil(t *testing.T) {
	result := PersonaSection(nil)
	if result != "" {
		t.Errorf("PersonaSection(nil) = %q, want empty", result)
	}
}

func TestPersonaSection_LowConfidence(t *testing.T) {
	p := memory.NewPersona("test", &memory.EnvironmentSignals{})
	p.Confidence = 0.1 // very low — budget < 50

	result := PersonaSection(p)
	if result != "" {
		t.Errorf("PersonaSection with low confidence = %q, want empty", result)
	}
}

func TestPersonaSection_WithKnowledge(t *testing.T) {
	env := &memory.EnvironmentSignals{Platform: "darwin", GitUserName: "alice"}
	p := memory.NewPersona("test", env)
	p.Confidence = 0.9
	p.Knowledge.AddOrUpdateDomain("Go", memory.LevelExpert, 0.9)
	p.Knowledge.AddOrUpdateDomain("Python", memory.LevelIntermediate, 0.6)

	result := PersonaSection(p)

	if !strings.HasPrefix(result, "# User context") {
		t.Errorf("should start with '# User context', got: %q", result[:30])
	}
	if !strings.Contains(result, "Go") {
		t.Error("should contain Go expertise")
	}
	if !strings.Contains(result, "Python") {
		t.Error("should contain Python expertise")
	}
}

func TestPersonaSection_WithCommunication(t *testing.T) {
	env := &memory.EnvironmentSignals{Platform: "darwin"}
	p := memory.NewPersona("test", env)
	p.Confidence = 0.9
	p.Communication.Verbosity = 0.1
	p.Communication.JustDoIt = true
	p.Communication.Confidence = 0.8

	result := PersonaSection(p)

	if !strings.Contains(result, "terse") {
		t.Error("should mention terse for low verbosity")
	}
	if !strings.Contains(result, "results over explanation") {
		t.Error("should mention just-do-it preference")
	}
}

func TestPersonaSection_WithSessionContext(t *testing.T) {
	env := &memory.EnvironmentSignals{Platform: "darwin"}
	p := memory.NewPersona("test", env)
	p.Confidence = 0.9
	p.SessionContext = &memory.SessionContext{
		DetectedRole: "debugging",
		DetectedMood: "focused",
	}

	result := PersonaSection(p)

	if !strings.Contains(result, "debugging mode") {
		t.Error("should contain session role")
	}
}

func TestPersonaSection_WithObservations(t *testing.T) {
	env := &memory.EnvironmentSignals{Platform: "darwin"}
	p := memory.NewPersona("test", env)
	p.Confidence = 0.9
	p.Interactions.AddObservation(memory.PersonaObservation{
		Text:       "Prefers table-driven tests",
		Category:   "preference",
		Confidence: 0.9,
		ObservedAt: time.Now(),
		Source:     "explicit",
	})

	result := PersonaSection(p)

	if !strings.Contains(result, "table-driven tests") {
		t.Error("should contain observation text")
	}
}

func TestPersonaSection_BudgetScaling(t *testing.T) {
	env := &memory.EnvironmentSignals{Platform: "darwin"}
	p := memory.NewPersona("test", env)
	p.Knowledge.AddOrUpdateDomain("Go", memory.LevelExpert, 0.9)
	p.Knowledge.AddOrUpdateDomain("Python", memory.LevelAdvanced, 0.8)
	p.Knowledge.AddOrUpdateDomain("Rust", memory.LevelIntermediate, 0.7)
	p.Knowledge.AddOrUpdateDomain("Java", memory.LevelNovice, 0.6)
	p.Communication.Verbosity = 0.1
	p.Communication.Confidence = 0.8
	p.Interactions.AddObservation(memory.PersonaObservation{
		Text:     "Very long observation that takes up a lot of space in the persona section",
		Category: "preference", Confidence: 0.9, ObservedAt: time.Now(), Source: "inferred",
	})

	// Full confidence: should have substantial content.
	p.Confidence = 1.0
	full := PersonaSection(p)

	// Half confidence: should have less content.
	p.Confidence = 0.5
	half := PersonaSection(p)

	if len(half) >= len(full) {
		t.Errorf("half-confidence output (%d chars) should be shorter than full (%d chars)", len(half), len(full))
	}
	if len(full) > MaxPersonaBudget {
		t.Errorf("full output (%d chars) exceeds MaxPersonaBudget (%d)", len(full), MaxPersonaBudget)
	}
}

func TestPersonaSection_EmptyPersona(t *testing.T) {
	// A fresh persona with defaults (0.5 confidence, no data) should produce minimal output.
	env := &memory.EnvironmentSignals{Platform: "darwin"}
	p := memory.NewPersona("test", env)

	result := PersonaSection(p)

	// Communication confidence is 0 by default, knowledge is empty,
	// session context has no role — should be empty.
	if result != "" {
		t.Errorf("empty persona should produce empty section, got: %q", result)
	}
}
