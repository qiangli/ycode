package ycode

import "github.com/qiangli/ycode/internal/tools"

// AgentMode names a runtime stance that maps to a curated tool allowlist.
//   - ModeBuild: full tool access (the default for a normal ycode agent).
//   - ModePlan: read-only planning workflow + plan-mode helpers.
//   - ModeExplore: pure read-only search and fetch tools.
//
// Use the slice returned by AllowedToolsForMode as the argument to
// WithBuiltinAllowlist.
type AgentMode = tools.AgentMode

const (
	ModeBuild   = tools.ModeBuild
	ModePlan    = tools.ModePlan
	ModeExplore = tools.ModeExplore
)

// AgentKind names a curated tool allowlist tailored to a subagent role.
type AgentKind = tools.AgentType

const (
	AgentExplore        = tools.AgentExplore
	AgentPlan           = tools.AgentPlan
	AgentVerification   = tools.AgentVerification
	AgentGeneralPurpose = tools.AgentGeneralPurpose
	AgentGuide          = tools.AgentGuide
	AgentStatusLine     = tools.AgentStatusLine
)

// AllowedToolsForMode returns the tool name allowlist for a runtime mode.
// Pass the result to WithBuiltinAllowlist. Returns nil for ModeBuild (which
// means "register every built-in").
func AllowedToolsForMode(mode AgentMode) []string {
	return tools.AllowedToolsForMode(mode)
}

// AllowedToolsForAgent returns the tool name allowlist tailored to an agent
// role (Explore, Plan, Verification, etc.).
func AllowedToolsForAgent(kind AgentKind) []string {
	return tools.AllowedToolsForAgent(kind)
}

// DefaultSubagentBlocklist lists tools that subagents are forbidden from
// using even when the mode/role allowlist would include them. Useful when
// composing host allowlists: AllowedToolsForMode minus DefaultSubagentBlocklist.
func DefaultSubagentBlocklist() []string {
	out := make([]string, len(tools.DefaultSubagentBlocklist))
	copy(out, tools.DefaultSubagentBlocklist)
	return out
}

// ApplyBlocklist returns a new slice containing every name in allowlist that
// is not present in blocklist. Pass nil allowlist to mean "all tools" (in
// which case the result is also nil — meaning all).
func ApplyBlocklist(allowlist, blocklist []string) []string {
	return tools.ApplyBlocklist(allowlist, blocklist)
}
