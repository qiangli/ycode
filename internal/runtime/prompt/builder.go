package prompt

import (
	"fmt"
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

// BuildDifferential assembles the system prompt, omitting unchanged dynamic
// sections when prompt caching is unavailable. Returns the prompt string and
// the section content map (for updating the baseline after a successful call).
func (b *Builder) BuildDifferential(baseline *ContextBaseline) (string, map[string]string) {
	// Collect current dynamic section contents for diffing.
	current := make(map[string]string)
	for _, s := range b.sections {
		if !s.Static {
			current[s.Name] = s.Content
		}
	}

	diff := baseline.Diff(current)

	var parts []string

	// Static sections always included.
	for _, s := range b.sections {
		if s.Static {
			parts = append(parts, s.Content)
		}
	}

	parts = append(parts, DynamicBoundary)

	if diff.IsFirst {
		// First turn: include everything.
		for _, s := range b.sections {
			if !s.Static {
				parts = append(parts, s.Content)
			}
		}
	} else {
		// Build a set of changed section names.
		changedSet := make(map[string]bool, len(diff.Changed))
		for _, name := range diff.Changed {
			changedSet[name] = true
		}

		// Include only changed sections; note omitted ones.
		if len(diff.Unchanged) > 0 {
			parts = append(parts, fmt.Sprintf("[Context: %d section(s) unchanged from previous turn, omitted to save tokens]", len(diff.Unchanged)))
		}
		for _, s := range b.sections {
			if !s.Static && changedSet[s.Name] {
				parts = append(parts, s.Content)
			}
		}
	}

	return strings.Join(parts, "\n\n"), current
}

// BuildDefault builds a system prompt with default sections and project context.
// When cachingSupported is false and a non-nil baseline is provided, uses
// differential context injection to omit unchanged dynamic sections.
func BuildDefault(ctx *ProjectContext, cachingSupported bool, baseline *ContextBaseline) string {
	b := NewBuilder()

	// Static sections (cacheable).
	b.AddStaticSection(SectionIntro, IntroSection())
	b.AddStaticSection(SectionSystem, SystemSection())
	b.AddStaticSection(SectionTasks, TasksSection())
	b.AddStaticSection(SectionActions, ActionsSection())

	// Dynamic sections.
	b.AddDynamicSection(SectionFilesystem, FilesystemSection(ctx.AllowedDirs))
	b.AddDynamicSection(SectionEnvironment, EnvironmentSection(ctx))
	b.AddDynamicSection(SectionProject, ProjectSection(ctx))
	b.AddDynamicSection(SectionGit, GitSection(ctx))
	b.AddDynamicSection(SectionInstructions, InstructionsSection(ctx.ContextFiles))

	// When caching is available (Anthropic), always send full prompt —
	// the static/dynamic boundary handles cache optimization.
	if cachingSupported || baseline == nil {
		return b.Build()
	}

	// Non-caching provider: use differential injection.
	prompt, sectionContents := b.BuildDifferential(baseline)
	baseline.Update(sectionContents)
	return prompt
}
