package skillengine

// TrustLevel indicates the provenance and trust of a skill.
// Inspired by paperclip's company skills trust levels.
type TrustLevel string

const (
	// TrustBuiltin means the skill is bundled with the binary.
	TrustBuiltin TrustLevel = "builtin"
	// TrustTrusted means the skill has been verified/approved.
	TrustTrusted TrustLevel = "trusted"
	// TrustUnverified means the skill was loaded from an untrusted source.
	TrustUnverified TrustLevel = "unverified"
)

// CompatibilitySpec defines which agents/providers a skill is compatible with
// and its trust level. This prevents skill injection into incompatible agents.
// Inspired by paperclip's per-adapter compatibility matrix.
type CompatibilitySpec struct {
	// CompatibleProviders lists provider types this skill works with.
	// Empty means compatible with all providers.
	// Example values: "anthropic", "openai", "ollama", "gemini"
	CompatibleProviders []string `json:"compatible_providers,omitempty" yaml:"compatible_providers,omitempty"`

	// Trust indicates skill provenance.
	Trust TrustLevel `json:"trust,omitempty" yaml:"trust,omitempty"`

	// MinContextWindow is the minimum context window (in tokens) required.
	// Skills that need large context (e.g., full codebase analysis) should set this.
	// 0 means no minimum.
	MinContextWindow int `json:"min_context_window,omitempty" yaml:"min_context_window,omitempty"`
}

// IsCompatibleWith checks if the skill is compatible with the given provider.
// Returns true if CompatibleProviders is empty (universal) or if the provider
// is in the list.
func (c *CompatibilitySpec) IsCompatibleWith(provider string) bool {
	if len(c.CompatibleProviders) == 0 {
		return true // universal compatibility
	}
	for _, p := range c.CompatibleProviders {
		if p == provider {
			return true
		}
	}
	return false
}

// FindCompatible returns skills from the list that are compatible with the
// given provider and have sufficient trust level.
func FindCompatible(skills []*SkillSpec, provider string, minTrust TrustLevel) []*SkillSpec {
	var result []*SkillSpec
	for _, s := range skills {
		if !s.Compatibility.IsCompatibleWith(provider) {
			continue
		}
		if minTrust != "" && !meetsMinTrust(s.Compatibility.Trust, minTrust) {
			continue
		}
		result = append(result, s)
	}
	return result
}

// meetsMinTrust checks if the skill's trust level meets the minimum.
// Order: builtin > trusted > unverified.
func meetsMinTrust(actual, minimum TrustLevel) bool {
	return trustRank(actual) >= trustRank(minimum)
}

func trustRank(t TrustLevel) int {
	switch t {
	case TrustBuiltin:
		return 3
	case TrustTrusted:
		return 2
	case TrustUnverified:
		return 1
	default:
		return 0 // unknown trust is lowest
	}
}
