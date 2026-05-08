package shell

// SkillResolver resolves the `@` sentinel — either to a registered skill
// (by identifier) or to a skill loaded from a filesystem path. The
// concrete implementation in skill_resolver.go wraps the existing skill
// discovery in `internal/tools` so the shell package does not have a
// hard dependency on the tools layer.
type SkillResolver interface {
	// Resolve looks up a skill by name and returns its rendered SKILL.md
	// content. The returned string is what the shell prints to the user
	// (skeleton behavior; LLM execution comes later).
	Resolve(name string) (string, error)

	// ResolvePath loads a skill from the given filesystem path.
	ResolvePath(path string) (string, error)

	// List returns the names of all known skills, sorted. Used by Tab
	// completion when the user has typed `@<prefix>`.
	List() []string
}
