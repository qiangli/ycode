// Package features implements the feature-tier registry that gates which
// capabilities ship in default builds vs. behind experimental/wip Go build
// tags. The registry is the single source of truth for "what is ready to
// ship." See docs/strategy.md#feature-tiers for the policy.
package features

// Tier classifies a feature's release readiness.
type Tier string

const (
	// TierStable: integration-tested, dogfooded, documented. In default builds,
	// README, benchmarks. Always compiled in.
	TierStable Tier = "stable"

	// TierExperimental: compiles, has tests, but new or rough. Opt-in via
	// `-tags experimental`. Emits a stderr warning on first invocation.
	// Excluded from README, benchmarks, default release artifacts.
	TierExperimental Tier = "experimental"

	// TierWIP: active development; may not work end-to-end. Opt-in via
	// `-tags wip`. Excluded from CI-default and from any user-facing surface.
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
	Name       string     `yaml:"name"`
	Tier       Tier       `yaml:"tier"`
	Files      []string   `yaml:"files,omitempty"`
	BlockedBy  []string   `yaml:"blocked_by,omitempty"`
	Notes      string     `yaml:"notes,omitempty"`
	Owner      string     `yaml:"owner,omitempty"`
	Graduation Graduation `yaml:"graduation,omitempty"`
}

// Registry holds the parsed feature list.
type Registry struct {
	Features []Feature `yaml:"features"`
}
