package memory

import (
	"testing"
	"time"
)

func TestPersona_SaveAndLoad(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	env := &EnvironmentSignals{
		Platform:    "darwin",
		Shell:       "zsh",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
		Hostname:    "macbook",
	}
	original := NewPersona("test-123", env)
	original.Confidence = 0.85
	original.Knowledge.AddOrUpdateDomain("Go", LevelExpert, 0.9)
	original.Knowledge.AddOrUpdateDomain("Python", LevelIntermediate, 0.6)
	original.Communication.Verbosity = 0.3
	original.Communication.Formality = 0.7
	original.Communication.JustDoIt = true
	original.Communication.Confidence = 0.8
	original.Behavior.ReviewsDiffs = 0.8
	original.Behavior.PrefersTDD = 0.9
	original.Interactions.TotalSessions = 42
	original.Interactions.TotalTurns = 500
	original.Interactions.AddObservation(PersonaObservation{
		Text:       "Prefers table-driven tests",
		Category:   "preference",
		Confidence: 0.9,
		ObservedAt: time.Now(),
		Source:     "explicit",
	})

	if err := SavePersona(store, original); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}

	loaded, err := LoadPersona(store, "test-123")
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadPersona returned nil")
	}

	// Verify frontmatter fields.
	if loaded.ID != "test-123" {
		t.Errorf("ID = %q, want %q", loaded.ID, "test-123")
	}
	if loaded.DisplayHint != "alice" {
		t.Errorf("DisplayHint = %q, want %q", loaded.DisplayHint, "alice")
	}
	if loaded.Confidence != 0.85 {
		t.Errorf("Confidence = %f, want 0.85", loaded.Confidence)
	}

	// Verify knowledge.
	if len(loaded.Knowledge.Domains) != 2 {
		t.Fatalf("Knowledge.Domains len = %d, want 2", len(loaded.Knowledge.Domains))
	}
	goDomain := loaded.Knowledge.FindDomain("Go")
	if goDomain == nil {
		t.Fatal("Go domain not found")
	}
	if goDomain.Level != LevelExpert {
		t.Errorf("Go level = %q, want %q", goDomain.Level, LevelExpert)
	}

	// Verify communication.
	if loaded.Communication.Verbosity != 0.3 {
		t.Errorf("Verbosity = %.2f, want 0.30", loaded.Communication.Verbosity)
	}
	if !loaded.Communication.JustDoIt {
		t.Error("JustDoIt should be true")
	}

	// Verify behavior.
	if loaded.Behavior.PrefersTDD != 0.9 {
		t.Errorf("PrefersTDD = %.2f, want 0.90", loaded.Behavior.PrefersTDD)
	}

	// Verify interactions.
	if loaded.Interactions.TotalSessions != 42 {
		t.Errorf("TotalSessions = %d, want 42", loaded.Interactions.TotalSessions)
	}
	if len(loaded.Interactions.Observations) != 1 {
		t.Fatalf("Observations len = %d, want 1", len(loaded.Interactions.Observations))
	}
	if loaded.Interactions.Observations[0].Text != "Prefers table-driven tests" {
		t.Errorf("Observation text = %q", loaded.Interactions.Observations[0].Text)
	}

	// Verify environment.
	if loaded.Environment.Platform != "darwin" {
		t.Errorf("Environment.Platform = %q, want %q", loaded.Environment.Platform, "darwin")
	}
}

func TestPersona_LoadNonexistent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	p, err := LoadPersona(store, "nonexistent")
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if p != nil {
		t.Error("expected nil for nonexistent persona")
	}
}

func TestListPersonas(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	env := &EnvironmentSignals{Platform: "linux"}

	// Create two personas.
	p1 := NewPersona("user-1", env)
	p2 := NewPersona("user-2", env)

	if err := SavePersona(store, p1); err != nil {
		t.Fatal(err)
	}
	if err := SavePersona(store, p2); err != nil {
		t.Fatal(err)
	}

	personas, err := ListPersonas(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(personas) != 2 {
		t.Errorf("ListPersonas len = %d, want 2", len(personas))
	}
}

func TestMigrateProfile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Create a legacy profile.
	profile := NewUserProfile()
	profile.Update("basic_info.name", "Bob")
	profile.Update("basic_info.role", "backend engineer")
	profile.Update("preferences.editor", "neovim")
	profile.Update("expertise", "Go")
	profile.Update("expertise", "Kubernetes")
	profile.Update("work_patterns", "TDD")

	if err := profile.Save(store); err != nil {
		t.Fatal(err)
	}

	env := &EnvironmentSignals{Platform: "linux", GitUserName: "bob"}
	p, err := MigrateProfile(store, env, "migrated-id")
	if err != nil {
		t.Fatalf("MigrateProfile: %v", err)
	}
	if p == nil {
		t.Fatal("MigrateProfile returned nil")
	}

	if p.DisplayHint != "Bob" {
		t.Errorf("DisplayHint = %q, want %q", p.DisplayHint, "Bob")
	}

	// Knowledge should have Go and Kubernetes.
	if len(p.Knowledge.Domains) != 2 {
		t.Errorf("Knowledge.Domains len = %d, want 2", len(p.Knowledge.Domains))
	}

	// Observations should include role, preference, and work pattern.
	if len(p.Interactions.Observations) < 3 {
		t.Errorf("Observations len = %d, want >= 3", len(p.Interactions.Observations))
	}
}

func TestMigrateProfile_Empty(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	env := &EnvironmentSignals{Platform: "linux"}
	p, err := MigrateProfile(store, env, "id")
	if err != nil {
		t.Fatal(err)
	}
	if p != nil {
		t.Error("expected nil for empty/nonexistent profile")
	}
}
