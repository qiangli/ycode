package skillengine

import "testing"

func TestCompatibilitySpec_UniversalCompatible(t *testing.T) {
	c := CompatibilitySpec{} // empty = universal
	if !c.IsCompatibleWith("anthropic") {
		t.Error("empty CompatibleProviders should be universal")
	}
	if !c.IsCompatibleWith("ollama") {
		t.Error("empty CompatibleProviders should be universal")
	}
}

func TestCompatibilitySpec_SpecificProviders(t *testing.T) {
	c := CompatibilitySpec{
		CompatibleProviders: []string{"anthropic", "openai"},
	}
	if !c.IsCompatibleWith("anthropic") {
		t.Error("should be compatible with listed provider")
	}
	if !c.IsCompatibleWith("openai") {
		t.Error("should be compatible with listed provider")
	}
	if c.IsCompatibleWith("ollama") {
		t.Error("should not be compatible with unlisted provider")
	}
}

func TestFindCompatible_FiltersProvider(t *testing.T) {
	skills := []*SkillSpec{
		{Name: "universal", Compatibility: CompatibilitySpec{}},
		{Name: "anthropic-only", Compatibility: CompatibilitySpec{
			CompatibleProviders: []string{"anthropic"},
		}},
		{Name: "openai-only", Compatibility: CompatibilitySpec{
			CompatibleProviders: []string{"openai"},
		}},
	}

	result := FindCompatible(skills, "anthropic", "")
	if len(result) != 2 {
		t.Errorf("anthropic compatible count = %d, want 2", len(result))
	}

	result = FindCompatible(skills, "ollama", "")
	if len(result) != 1 {
		t.Errorf("ollama compatible count = %d, want 1 (universal only)", len(result))
	}
}

func TestFindCompatible_FiltersTrust(t *testing.T) {
	skills := []*SkillSpec{
		{Name: "builtin", Compatibility: CompatibilitySpec{Trust: TrustBuiltin}},
		{Name: "trusted", Compatibility: CompatibilitySpec{Trust: TrustTrusted}},
		{Name: "unverified", Compatibility: CompatibilitySpec{Trust: TrustUnverified}},
	}

	result := FindCompatible(skills, "", TrustTrusted)
	if len(result) != 2 {
		t.Errorf("trusted+ count = %d, want 2 (builtin + trusted)", len(result))
	}

	result = FindCompatible(skills, "", TrustBuiltin)
	if len(result) != 1 {
		t.Errorf("builtin+ count = %d, want 1", len(result))
	}

	result = FindCompatible(skills, "", TrustUnverified)
	if len(result) != 3 {
		t.Errorf("unverified+ count = %d, want 3 (all)", len(result))
	}
}

func TestMeetsMinTrust(t *testing.T) {
	tests := []struct {
		actual  TrustLevel
		minimum TrustLevel
		want    bool
	}{
		{TrustBuiltin, TrustBuiltin, true},
		{TrustBuiltin, TrustTrusted, true},
		{TrustBuiltin, TrustUnverified, true},
		{TrustTrusted, TrustBuiltin, false},
		{TrustTrusted, TrustTrusted, true},
		{TrustTrusted, TrustUnverified, true},
		{TrustUnverified, TrustTrusted, false},
		{TrustUnverified, TrustUnverified, true},
		{"", TrustUnverified, false}, // unknown is lowest
	}
	for _, tt := range tests {
		got := meetsMinTrust(tt.actual, tt.minimum)
		if got != tt.want {
			t.Errorf("meetsMinTrust(%q, %q) = %v, want %v", tt.actual, tt.minimum, got, tt.want)
		}
	}
}
