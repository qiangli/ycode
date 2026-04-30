// Package contract provides deterministic validation tests for ycode's
// infrastructure. These tests run without LLM, network, or containers.
package contract

import (
	"testing"
	"time"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

// =============================================================================
// Persona: Validate persona model, storage, observation, and resolution
// =============================================================================

func TestPersona_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persona lifecycle test in short mode")
	}

	store, err := memory.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	env := &memory.EnvironmentSignals{
		Platform:    "darwin",
		Shell:       "zsh",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
		Hostname:    "macbook",
	}

	// Phase 1: Resolve creates a new persona.
	resolver := memory.NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.ID == "" {
		t.Fatal("persona should have a non-empty ID")
	}
	if p.Confidence != 0.5 {
		t.Errorf("initial confidence = %.2f, want 0.5", p.Confidence)
	}

	// Phase 2: Simulate a session with signals.
	for i := range 12 {
		sig := memory.ObserveTurn(
			"fix the nil pointer error in the handler",
			[]memory.ToolOutcome{{ToolName: "bash", Approved: true}},
			i,
		)
		p.SessionContext.Update(sig)
	}

	// Role should be detected after enough signals.
	if p.SessionContext.DetectedRole != "debugging" {
		t.Errorf("DetectedRole = %q, want %q", p.SessionContext.DetectedRole, "debugging")
	}

	// Phase 3: Session-end update.
	memory.UpdatePersonaFromSession(p)

	if p.Interactions.TotalSessions != 1 {
		t.Errorf("TotalSessions = %d, want 1", p.Interactions.TotalSessions)
	}
	if p.Interactions.TotalTurns != 12 {
		t.Errorf("TotalTurns = %d, want 12", p.Interactions.TotalTurns)
	}
	if p.Communication.Confidence <= 0 {
		t.Error("Communication.Confidence should be > 0 after a session")
	}

	// Phase 4: Save and reload.
	if err := memory.SavePersona(store, p); err != nil {
		t.Fatalf("SavePersona: %v", err)
	}

	loaded, err := memory.LoadPersona(store, p.ID)
	if err != nil {
		t.Fatalf("LoadPersona: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded persona is nil")
	}
	if loaded.Interactions.TotalSessions != 1 {
		t.Errorf("loaded TotalSessions = %d, want 1", loaded.Interactions.TotalSessions)
	}

	// Phase 5: Subsequent resolve should match existing persona.
	p2, err := resolver.Resolve(env)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if p2.ID != p.ID {
		t.Errorf("second resolve ID = %q, want %q (should match first)", p2.ID, p.ID)
	}
	if p2.Confidence < 0.9 {
		t.Errorf("second resolve confidence = %.2f, want >= 0.9 for exact match", p2.Confidence)
	}
}

func TestPersona_ObserverSignalExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persona observer test in short mode")
	}

	tests := []struct {
		name           string
		message        string
		wantIntent     string
		minTechDensity float64
		minQuestions   int
	}{
		{
			name:       "debugging message",
			message:    "fix the panic in the error handler, the stack trace shows a nil pointer",
			wantIntent: "debugging",
		},
		{
			name:         "learning message",
			message:      "explain how the goroutine scheduler works, help me understand the context package",
			wantIntent:   "learning",
			minQuestions: 0,
		},
		{
			name:           "technical message",
			message:        "add a goroutine with mutex and channel for the grpc api handler endpoint",
			minTechDensity: 0.3,
		},
		{
			name:       "architecture message",
			message:    "should we refactor this to a different design pattern for scalability",
			wantIntent: "architecting",
		},
		{
			name:       "review message",
			message:    "review the pull request, the diff and changes look good, approve it",
			wantIntent: "reviewing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig := memory.ObserveTurn(tt.message, nil, 0)

			if tt.wantIntent != "" && sig.DetectedIntent != tt.wantIntent {
				t.Errorf("DetectedIntent = %q, want %q", sig.DetectedIntent, tt.wantIntent)
			}
			if tt.minTechDensity > 0 && sig.TechnicalDensity < tt.minTechDensity {
				t.Errorf("TechnicalDensity = %.2f, want >= %.2f", sig.TechnicalDensity, tt.minTechDensity)
			}
			if sig.MessageLength == 0 {
				t.Error("MessageLength should be > 0")
			}
		})
	}
}

func TestPersona_IdentityResolution_MultiUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persona multi-user test in short mode")
	}

	store, err := memory.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	resolver := memory.NewPersonaResolver(store, nil)

	// User A.
	envA := &memory.EnvironmentSignals{
		Platform:    "darwin",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
	}
	pA, err := resolver.Resolve(envA)
	if err != nil {
		t.Fatal(err)
	}

	// User B — different person, same machine type.
	envB := &memory.EnvironmentSignals{
		Platform:    "darwin",
		GitUserName: "bob",
		GitEmail:    "bob@example.com",
		HomeDir:     "/Users/bob",
	}
	pB, err := resolver.Resolve(envB)
	if err != nil {
		t.Fatal(err)
	}

	// Should be different personas.
	if pA.ID == pB.ID {
		t.Error("different users should get different persona IDs")
	}

	// Should have 2 personas in store.
	personas, err := memory.ListPersonas(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(personas) != 2 {
		t.Errorf("stored personas = %d, want 2", len(personas))
	}

	// Re-resolving user A should match the original.
	pA2, err := resolver.Resolve(envA)
	if err != nil {
		t.Fatal(err)
	}
	if pA2.ID != pA.ID {
		t.Errorf("re-resolved alice ID = %q, want %q", pA2.ID, pA.ID)
	}
}

func TestPersona_ProfileMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persona migration test in short mode")
	}

	store, err := memory.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Create legacy profile.
	profile := memory.NewUserProfile()
	profile.Update("basic_info.name", "Charlie")
	profile.Update("basic_info.role", "SRE")
	profile.Update("preferences.editor", "vim")
	profile.Update("expertise", "Kubernetes")
	profile.Update("expertise", "Go")
	profile.Update("work_patterns", "incident response")
	if err := profile.Save(store); err != nil {
		t.Fatal(err)
	}

	// Resolve should migrate.
	env := &memory.EnvironmentSignals{
		Platform:    "linux",
		GitUserName: "charlie",
		HomeDir:     "/home/charlie",
	}
	resolver := memory.NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(env)
	if err != nil {
		t.Fatal(err)
	}

	// Verify migration.
	if p.DisplayHint != "Charlie" {
		t.Errorf("DisplayHint = %q, want %q", p.DisplayHint, "Charlie")
	}

	// Knowledge domains from expertise.
	if len(p.Knowledge.Domains) != 2 {
		t.Errorf("Knowledge.Domains = %d, want 2", len(p.Knowledge.Domains))
	}

	// Observations from role, preferences, work patterns.
	if len(p.Interactions.Observations) < 3 {
		t.Errorf("Observations = %d, want >= 3", len(p.Interactions.Observations))
	}

	// Verify specific observation content.
	foundRole := false
	for _, obs := range p.Interactions.Observations {
		if obs.Text == "Role: SRE" {
			foundRole = true
		}
	}
	if !foundRole {
		t.Error("should have migrated role as an observation")
	}
}

func TestPersona_SessionContextRoleStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persona role stability test in short mode")
	}

	sc := memory.NewSessionContext()

	// First 6 turns: debugging.
	for i := range 6 {
		sc.Update(memory.SessionSignal{
			TurnNumber:     i,
			DetectedIntent: "debugging",
			Timestamp:      time.Now(),
		})
	}
	if sc.DetectedRole != "debugging" {
		t.Errorf("after debugging turns, role = %q, want %q", sc.DetectedRole, "debugging")
	}

	// Next 9 turns: learning (should switch after majority changes).
	for i := range 9 {
		sc.Update(memory.SessionSignal{
			TurnNumber:     6 + i,
			DetectedIntent: "learning",
			Timestamp:      time.Now(),
		})
	}
	if sc.DetectedRole != "learning" {
		t.Errorf("after learning turns, role = %q, want %q", sc.DetectedRole, "learning")
	}
	if sc.TurnsSinceSwitch > 9 {
		t.Errorf("TurnsSinceSwitch = %d, should have reset on role change", sc.TurnsSinceSwitch)
	}
}

func TestPersona_StorageRoundtrip_AllFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping persona roundtrip test in short mode")
	}

	store, err := memory.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	env := &memory.EnvironmentSignals{
		Platform:    "linux",
		Shell:       "bash",
		GitUserName: "dave",
		GitEmail:    "dave@example.com",
		HomeDir:     "/home/dave",
		Hostname:    "server-01",
	}

	p := memory.NewPersona("roundtrip-test", env)
	p.Confidence = 0.92
	p.Knowledge.AddOrUpdateDomain("Rust", memory.LevelAdvanced, 0.85)
	p.Knowledge.AddOrUpdateDomain("Python", memory.LevelExpert, 0.95)
	p.Communication.Verbosity = 0.2
	p.Communication.Formality = 0.8
	p.Communication.JustDoIt = true
	p.Communication.AsksClarify = false
	p.Communication.Confidence = 0.75
	p.Behavior.ReviewsDiffs = 0.9
	p.Behavior.PrefersTDD = 0.7
	p.Behavior.ToolApprovalRate = 0.85
	p.Behavior.CorrectionFreq = 0.15
	p.Behavior.QuestionToCommand = 0.3
	p.Behavior.AvgSessionMinutes = 45.5
	p.Behavior.TopicBreadth = 0.4
	p.Interactions.TotalSessions = 100
	p.Interactions.TotalTurns = 5000
	p.Interactions.AddObservation(memory.PersonaObservation{
		Text:       "Prefers unsafe blocks over wrappers",
		Category:   "preference",
		Confidence: 0.88,
		ObservedAt: time.Now(),
		Source:     "inferred",
	})
	p.Interactions.AddObservation(memory.PersonaObservation{
		Text:       "Explicitly requested no emojis",
		Category:   "communication",
		Confidence: 1.0,
		ObservedAt: time.Now(),
		Source:     "explicit",
	})

	if err := memory.SavePersona(store, p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := memory.LoadPersona(store, "roundtrip-test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded persona is nil")
	}

	// Verify all fields survived roundtrip.
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"ID", loaded.ID, "roundtrip-test"},
		{"DisplayHint", loaded.DisplayHint, "dave"},
		{"Confidence", loaded.Confidence, 0.92},
		{"Knowledge.Domains.len", len(loaded.Knowledge.Domains), 2},
		{"Communication.Verbosity", loaded.Communication.Verbosity, 0.2},
		{"Communication.Formality", loaded.Communication.Formality, 0.8},
		{"Communication.JustDoIt", loaded.Communication.JustDoIt, true},
		{"Communication.Confidence", loaded.Communication.Confidence, 0.75},
		{"Behavior.ReviewsDiffs", loaded.Behavior.ReviewsDiffs, 0.9},
		{"Behavior.PrefersTDD", loaded.Behavior.PrefersTDD, 0.7},
		{"Behavior.AvgSessionMinutes", loaded.Behavior.AvgSessionMinutes, 45.5},
		{"Interactions.TotalSessions", loaded.Interactions.TotalSessions, 100},
		{"Interactions.TotalTurns", loaded.Interactions.TotalTurns, 5000},
		{"Interactions.Observations.len", len(loaded.Interactions.Observations), 2},
		{"Environment.Platform", loaded.Environment.Platform, "linux"},
		{"Environment.GitUserName", loaded.Environment.GitUserName, "dave"},
		{"Environment.Hostname", loaded.Environment.Hostname, "server-01"},
	}

	for _, c := range checks {
		switch got := c.got.(type) {
		case float64:
			want := c.want.(float64)
			if got != want {
				t.Errorf("%s = %v, want %v", c.name, got, want)
			}
		case int:
			want := c.want.(int)
			if got != want {
				t.Errorf("%s = %v, want %v", c.name, got, want)
			}
		case string:
			want := c.want.(string)
			if got != want {
				t.Errorf("%s = %q, want %q", c.name, got, want)
			}
		case bool:
			want := c.want.(bool)
			if got != want {
				t.Errorf("%s = %v, want %v", c.name, got, want)
			}
		}
	}
}
