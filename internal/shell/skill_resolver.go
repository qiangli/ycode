package shell

import (
	"sort"

	"github.com/qiangli/ycode/internal/tools"
)

// toolsSkillResolver wraps the existing skill discovery in internal/tools
// behind the shell.SkillResolver interface. Per plan §13f the skeleton
// keeps the discovery logic in `internal/tools` and exposes it through
// thin exported entry points (DiscoverSkill, LoadSkillFromPath, ListSkills).
type toolsSkillResolver struct{}

// NewSkillResolver returns the default SkillResolver, which delegates to
// internal/tools for discovery.
func NewSkillResolver() SkillResolver { return toolsSkillResolver{} }

func (toolsSkillResolver) Resolve(name string) (string, error) {
	return tools.DiscoverSkill(name)
}

func (toolsSkillResolver) ResolvePath(path string) (string, error) {
	return tools.LoadSkillFromPath(path)
}

func (toolsSkillResolver) List() []string {
	skills := tools.ListSkills()
	sort.Strings(skills)
	return skills
}
