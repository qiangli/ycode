// Package features implements the feature-tier registry. Tier is a
// release-readiness label, not a build-time switch — all features
// compile unconditionally. The tiering may be revisited if/when ycode
// has enough users to warrant gating new features behind opt-in tags.
package features

// Tier classifies a feature's release readiness.
type Tier string

const (
	// TierStable: integration-tested, dogfooded, documented.
	TierStable Tier = "stable"

	// TierExperimental: compiles unconditionally, but new or rough.
	// Surfaced in `ycode features list` so users know what's not yet
	// hardened. Excluded from README marketing material.
	TierExperimental Tier = "experimental"

	// TierWIP: active development; may not work end-to-end. Surfaced in
	// `ycode features list` for transparency; excluded from user-facing
	// docs.
	TierWIP Tier = "wip"
)

// Valid reports whether t is a recognized tier.
func (t Tier) Valid() bool {
	switch t {
	case TierStable, TierExperimental, TierWIP:
		return true
	}
	return false
}

// Graduation captures the criteria a feature must satisfy to be classified
// stable. CI verifies these for tier=stable entries before each release.
type Graduation struct {
	IntegrationTest bool `yaml:"integration_test"`
	DogfoodWeeks    int  `yaml:"dogfood_weeks"`
	FailureModeDoc  bool `yaml:"failure_mode_doc"`
}

// Feature is a single registry entry.
type Feature struct {
	Name string `yaml:"name"`
	Tier Tier   `yaml:"tier"`
	// Description is a single short phrase for user-facing surfaces (README,
	// website, marketing). Stable features should have a non-empty
	// description; experimental/wip may omit it.
	Description string     `yaml:"description,omitempty"`
	Files       []string   `yaml:"files,omitempty"`
	BlockedBy   []string   `yaml:"blocked_by,omitempty"`
	Notes       string     `yaml:"notes,omitempty"`
	Owner       string     `yaml:"owner,omitempty"`
	Graduation  Graduation `yaml:"graduation,omitempty"`
}

// Registry holds the parsed feature list.
type Registry struct {
	Features []Feature `yaml:"features"`
}
