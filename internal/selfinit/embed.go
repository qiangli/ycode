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
