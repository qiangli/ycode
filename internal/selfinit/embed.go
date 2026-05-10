package selfinit

import _ "embed"

// Foreman protocol assets embedded in the binary. Written into every
// repo by WriteProjectFiles so the protocol is universally available
// without manual file copies. The canonical source for each is the
// matching .md in internal/selfinit/embed/.

//go:embed embed/foreman_skill.md
var foremanSkillMD string

//go:embed embed/backlog_protocol.md
var backlogProtocolMD string

// ForemanSkillBody returns the embedded /foreman skill body. Useful
// to callers that want to inject the skill into a runtime registry
// without touching the filesystem.
func ForemanSkillBody() string { return foremanSkillMD }

// BacklogProtocolDoc returns the embedded docs/backlog.md content.
func BacklogProtocolDoc() string { return backlogProtocolMD }

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
// new universal skills land. Project-specific ycode skills (those
// that only make sense inside the ycode source tree, e.g. /build,
// /deploy, /eval, /analyze) are NOT listed here — they stay scoped
// to ycode's own .agents/ycode/skills/.
var SkillInventory = []SkillInventoryEntry{
	{
		Name:     "/foreman",
		Summary:  "Boss → Foreman → Worker autonomous task loop",
		BodyPath: "~/.config/ycode/skills/ycode-foreman/skill.md",
		When:     "you start a session with no specific user task; pick up the next prioritized item from docs/backlog/ and ship it",
	},
}
