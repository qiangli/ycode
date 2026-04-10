package prompt

import (
	"strings"
)

// Section is a named section of the system prompt.
type Section struct {
	Name    string
	Content string
	Static  bool // true = before dynamic boundary (cacheable)
}

// Builder assembles the system prompt from sections.
type Builder struct {
	sections []Section
}

// NewBuilder creates a new system prompt builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// AddStaticSection adds a section before the dynamic boundary.
func (b *Builder) AddStaticSection(name, content string) {
	if content == "" {
		return
	}
	b.sections = append(b.sections, Section{Name: name, Content: content, Static: true})
}

// AddDynamicSection adds a section after the dynamic boundary.
func (b *Builder) AddDynamicSection(name, content string) {
	if content == "" {
		return
	}
	b.sections = append(b.sections, Section{Name: name, Content: content, Static: false})
}

// Build assembles the full system prompt.
func (b *Builder) Build() string {
	var parts []string

	// Static sections first.
	for _, s := range b.sections {
		if s.Static {
			parts = append(parts, s.Content)
		}
	}

	// Dynamic boundary.
	parts = append(parts, DynamicBoundary)

	// Dynamic sections.
	for _, s := range b.sections {
		if !s.Static {
			parts = append(parts, s.Content)
		}
	}

	return strings.Join(parts, "\n\n")
}

// BuildDefault builds a system prompt with default sections and project context.
func BuildDefault(ctx *ProjectContext) string {
	b := NewBuilder()

	// Static sections (cacheable).
	b.AddStaticSection(SectionIntro, IntroSection())
	b.AddStaticSection(SectionSystem, SystemSection())
	b.AddStaticSection(SectionTasks, TasksSection())
	b.AddStaticSection(SectionActions, ActionsSection())

	// Dynamic sections.
	b.AddDynamicSection(SectionEnvironment, EnvironmentSection(ctx))
	b.AddDynamicSection(SectionProject, ProjectSection(ctx))
	b.AddDynamicSection(SectionGit, GitSection(ctx))
	b.AddDynamicSection(SectionInstructions, InstructionsSection(ctx.ContextFiles))

	return b.Build()
}
