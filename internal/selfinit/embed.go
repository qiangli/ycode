package selfinit

import "github.com/qiangli/ycode/skills"

// The skill bodies shipped with the binary live in the top-level
// skills/ package (skills/ycode-<name>/skill.md, compiled in via
// embedding). WriteUserSkills installs editable copies under
// ~/.config/ycode/skills/ — never into a repo.

// ForemanSkillBody returns the embedded /foreman skill body. Useful
// to callers that want to inject the skill into a runtime registry
// without touching the filesystem.
func ForemanSkillBody() string {
	body, _ := skills.Body("ycode-foreman")
	return body
}

// SkillInventoryEntry is one row in the "Skills available via ycode"
// table that gets rendered into .agents/ycode/AGENTS.md by every
// `ycode init`. Foreign agents (Claude Code, OpenCode, Codex, …)
// discover ycode's skills by reading that file — ycode does NOT write
// into their personal directories. The inventory is the non-intrusive
// pull surface.
type SkillInventoryEntry struct {
	Name     string // slash-command form, e.g. "/foreman"
	Summary  string // one-liner, sentence case, no trailing period
	BodyPath string // canonical on-disk location (user-global) or "embedded"
	When     string // when an agent should reach for this skill
}

// SkillInventory is the registry of universal ycode skills surfaced
// to foreign agents via .agents/ycode/AGENTS.md. Append entries as
// new universal skills land. ALL embedded skills are installed
// user-globally by WriteUserSkills; this curated list is the subset
// worth advertising to foreign agents (deep ycode-development skills
// like /build or /deploy are installed too but only matter inside
// the ycode source tree).
var SkillInventory = []SkillInventoryEntry{
	{
		Name:     "/foreman",
		Summary:  "Boss → Foreman → Worker autonomous task loop",
		BodyPath: "~/.config/ycode/skills/ycode-foreman/skill.md",
		When:     "you start a session with no specific user task; pick up the next prioritized item from the project backlog (`ycode backlog list`) and ship it",
	},
	{
		Name:     "/autopilot",
		Summary:  "Autonomously execute a development task through a research-plan-build-test-fix-commit loop",
		BodyPath: "~/.config/ycode/skills/ycode-autopilot/skill.md",
		When:     "a single development task should run end-to-end without approval stops",
	},
}
