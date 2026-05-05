package memory

import (
	"testing"
)

func TestMatchScore(t *testing.T) {
	stored := &EnvironmentSignals{
		Platform:    "darwin",
		Shell:       "zsh",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
		Hostname:    "macbook",
	}

	tests := []struct {
		name    string
		current *EnvironmentSignals
		wantMin float64
		wantMax float64
	}{
		{
			name:    "exact match",
			current: stored,
			wantMin: 0.99,
			wantMax: 1.01,
		},
		{
			name: "same git user and email",
			current: &EnvironmentSignals{
				Platform:    "linux",
				Shell:       "bash",
				GitUserName: "alice",
				GitEmail:    "alice@example.com",
				HomeDir:     "/home/alice",
				Hostname:    "server",
			},
			wantMin: 0.64, // git_user(0.35) + git_email(0.30) = 0.65
			wantMax: 0.66,
		},
		{
			name: "only platform matches",
			current: &EnvironmentSignals{
				Platform:    "darwin",
				Shell:       "bash",
				GitUserName: "bob",
				GitEmail:    "bob@example.com",
				HomeDir:     "/Users/bob",
				Hostname:    "other",
			},
			wantMin: 0.09,
			wantMax: 0.11,
		},
		{
			name:    "nothing matches",
			current: &EnvironmentSignals{},
			wantMin: -0.01,
			wantMax: 0.01,
		},
		{
			name:    "nil stored",
			current: nil,
			wantMin: -0.01,
			wantMax: 0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := matchScore(stored, tt.current)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("matchScore = %.4f, want [%.2f, %.2f]", score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestPersonaResolver_Resolve_NewPersona(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	env := &EnvironmentSignals{
		Platform:    "darwin",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
	}

	resolver := NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p == nil {
		t.Fatal("Resolve returned nil")
	}
	if p.DisplayHint != "alice" {
		t.Errorf("DisplayHint = %q, want %q", p.DisplayHint, "alice")
	}
	if p.SessionContext == nil {
		t.Error("SessionContext should be initialized")
	}

	// Verify it was persisted.
	personas, _ := ListPersonas(store)
	if len(personas) != 1 {
		t.Errorf("persisted personas = %d, want 1", len(personas))
	}
}

func TestPersonaResolver_Resolve_ExistingMatch(t *testing.T) {
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

	// Create an existing persona.
	existing := NewPersona("existing-id", env)
	existing.Knowledge.AddOrUpdateDomain("Go", LevelExpert, 0.9)
	if err := SavePersona(store, existing); err != nil {
		t.Fatal(err)
	}

	// Resolve with same environment.
	resolver := NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if p.ID != "existing-id" {
		t.Errorf("ID = %q, want %q (should match existing)", p.ID, "existing-id")
	}
	if p.Confidence < 0.9 {
		t.Errorf("Confidence = %.2f, want >= 0.9 for exact match", p.Confidence)
	}
	if len(p.Knowledge.Domains) != 1 {
		t.Errorf("Knowledge.Domains len = %d, want 1 (should preserve existing data)", len(p.Knowledge.Domains))
	}
}

func TestPersonaResolver_Resolve_LowConfidence(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	storedEnv := &EnvironmentSignals{
		Platform:    "darwin",
		Shell:       "zsh",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
		Hostname:    "macbook",
	}
	existing := NewPersona("existing-id", storedEnv)
	if err := SavePersona(store, existing); err != nil {
		t.Fatal(err)
	}

	// Resolve with partially matching environment (only git user matches).
	currentEnv := &EnvironmentSignals{
		Platform:    "linux",
		Shell:       "bash",
		GitUserName: "alice",
		HomeDir:     "/home/alice-work",
		Hostname:    "server",
	}

	resolver := NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(currentEnv)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Should still match (score = 0.35 for git username >= 0.3 threshold).
	if p.ID != "existing-id" {
		t.Errorf("ID = %q, want %q", p.ID, "existing-id")
	}
	if p.Confidence >= 0.6 {
		t.Errorf("Confidence = %.2f, should be < 0.6 for weak match", p.Confidence)
	}
}

func TestPersonaResolver_Resolve_NoMatch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	storedEnv := &EnvironmentSignals{
		Platform:    "darwin",
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
	}
	existing := NewPersona("alice-id", storedEnv)
	if err := SavePersona(store, existing); err != nil {
		t.Fatal(err)
	}

	// Completely different user.
	currentEnv := &EnvironmentSignals{
		Platform:    "linux",
		GitUserName: "bob",
		GitEmail:    "bob@example.com",
		HomeDir:     "/home/bob",
	}

	resolver := NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(currentEnv)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Should create a new persona, not match alice.
	if p.ID == "alice-id" {
		t.Error("should not match alice's persona")
	}
	if p.DisplayHint != "bob" {
		t.Errorf("DisplayHint = %q, want %q", p.DisplayHint, "bob")
	}

	// Now there should be 2 personas.
	personas, _ := ListPersonas(store)
	if len(personas) != 2 {
		t.Errorf("persisted personas = %d, want 2", len(personas))
	}
}

func TestPersonaResolver_Resolve_MigratesProfile(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Create a legacy profile.
	profile := NewUserProfile()
	profile.Update("basic_info.name", "Charlie")
	profile.Update("expertise", "Go")
	if err := profile.Save(store); err != nil {
		t.Fatal(err)
	}

	env := &EnvironmentSignals{
		Platform:    "darwin",
		GitUserName: "charlie",
		HomeDir:     "/Users/charlie",
	}

	resolver := NewPersonaResolver(store, nil)
	p, err := resolver.Resolve(env)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Should have migrated profile data.
	if p.DisplayHint != "Charlie" {
		t.Errorf("DisplayHint = %q, want %q", p.DisplayHint, "Charlie")
	}
	if len(p.Knowledge.Domains) != 1 {
		t.Errorf("Knowledge.Domains = %d, want 1 (migrated from profile)", len(p.Knowledge.Domains))
	}
}

func TestEnvHash_Deterministic(t *testing.T) {
	env := &EnvironmentSignals{
		GitUserName: "alice",
		GitEmail:    "alice@example.com",
		HomeDir:     "/Users/alice",
	}

	h1 := envHash(env)
	h2 := envHash(env)
	if h1 != h2 {
		t.Errorf("envHash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Errorf("envHash length = %d, want 16", len(h1))
	}
}

func TestEnvHash_DifferentForDifferentUsers(t *testing.T) {
	env1 := &EnvironmentSignals{GitUserName: "alice", GitEmail: "alice@example.com", HomeDir: "/Users/alice"}
	env2 := &EnvironmentSignals{GitUserName: "bob", GitEmail: "bob@example.com", HomeDir: "/Users/bob"}

	if envHash(env1) == envHash(env2) {
		t.Error("envHash should differ for different users")
	}
}
