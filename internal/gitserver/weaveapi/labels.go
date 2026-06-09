// Package weaveapi bundles the v2 Loom-specific Gitea operations that
// live above the generic gitserver.Client. It owns the `loom:*` label
// namespace (state, priority, source) and the sticky-comment manager,
// the two surfaces every weave subverb writes to.
//
// Everything goes through the stable v1 REST API; project boards
// (Gitea-1.26 web-routes only, no REST) are scoped to the opt-in
// `ycode weave init-board` command, not used by this package.
package weaveapi

// State labels — loom moves an issue through these as the lease
// progresses. Owned by loom: no other code should set or remove them.
const (
	LabelStateTodo      = "loom:todo"
	LabelStateWorking   = "loom:working"
	LabelStateSubmitted = "loom:submitted"
	LabelStateCIFailed  = "loom:ci-failed"
	LabelStateConflict  = "loom:conflict"
	LabelStateMerged    = "loom:merged"
	LabelStateAbandoned = "loom:abandoned"

	// LabelProposed marks an agent-filed issue parked for human review
	// when `seeding.agent_filed_default_state: proposed` is enabled.
	// Excluded from `weave start` candidates.
	LabelProposed = "loom:proposed"
)

// Priority labels — coarse tier. Default for new issues is p2.
// Sorted by ascending priority value (p0 = highest).
const (
	LabelPriorityP0 = "loom:p0"
	LabelPriorityP1 = "loom:p1"
	LabelPriorityP2 = "loom:p2"
	LabelPriorityP3 = "loom:p3"
)

// Source labels — auto-applied by weave_add / weave add based on the
// caller context.
const (
	LabelSourceHuman = "loom:source:human"
	LabelSourceAgent = "loom:source:agent"
)

// LabelSpec pairs a label name with a Gitea color (hex w/o #) for
// first-run setup.
type LabelSpec struct {
	Name  string
	Color string // hex without leading #
}

// StateLabelSpecs returns the canonical state-label set to bootstrap
// into a Gitea repo at first run.
func StateLabelSpecs() []LabelSpec {
	return []LabelSpec{
		{LabelStateTodo, "808080"},      // gray
		{LabelStateWorking, "1d76db"},   // blue
		{LabelStateSubmitted, "0e8a16"}, // green
		{LabelStateCIFailed, "b60205"},  // dark red
		{LabelStateConflict, "fbca04"},  // yellow
		{LabelStateMerged, "5319e7"},    // purple
		{LabelStateAbandoned, "cccccc"}, // light gray
		{LabelProposed, "fef2c0"},       // pale yellow
	}
}

// PriorityLabelSpecs returns the four priority-tier labels.
func PriorityLabelSpecs() []LabelSpec {
	return []LabelSpec{
		{LabelPriorityP0, "d93f0b"}, // red
		{LabelPriorityP1, "fbca04"}, // orange/yellow
		{LabelPriorityP2, "fef2c0"}, // pale yellow
		{LabelPriorityP3, "ededed"}, // gray
	}
}

// SourceLabelSpecs returns the source-attribution labels.
func SourceLabelSpecs() []LabelSpec {
	return []LabelSpec{
		{LabelSourceHuman, "c2e0c6"}, // pale green
		{LabelSourceAgent, "bfdadc"}, // pale teal
	}
}

// AllLabelSpecs returns every loom-owned label bootstrap should create.
func AllLabelSpecs() []LabelSpec {
	out := make([]LabelSpec, 0, 16)
	out = append(out, StateLabelSpecs()...)
	out = append(out, PriorityLabelSpecs()...)
	out = append(out, SourceLabelSpecs()...)
	return out
}

// PriorityValue maps a tier label to its sort value. Unrecognized
// (including the empty string) returns 2, matching the design's
// "unlabeled defaults to p2" rule.
func PriorityValue(label string) int {
	switch label {
	case LabelPriorityP0:
		return 0
	case LabelPriorityP1:
		return 1
	case LabelPriorityP2:
		return 2
	case LabelPriorityP3:
		return 3
	default:
		return 2
	}
}

// IsPriorityLabel reports whether a label name is one of the four
// loom priority labels.
func IsPriorityLabel(name string) bool {
	switch name {
	case LabelPriorityP0, LabelPriorityP1, LabelPriorityP2, LabelPriorityP3:
		return true
	}
	return false
}

// IsStateLabel reports whether a label is one of the loom-owned state
// labels (used for state-machine transitions).
func IsStateLabel(name string) bool {
	switch name {
	case LabelStateTodo, LabelStateWorking, LabelStateSubmitted,
		LabelStateCIFailed, LabelStateConflict, LabelStateMerged,
		LabelStateAbandoned, LabelProposed:
		return true
	}
	return false
}
