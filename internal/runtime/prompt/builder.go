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

// BuildDifferential assembles the system prompt, omitting unchanged sections
// when prompt caching is unavailable. On first turn, everything is sent.
// On subsequent turns, both static and dynamic sections that haven't changed
// are omitted — this saves ~1,500+ tokens/turn for non-caching providers.
//
// Returns the prompt string and the section content map (for updating the
// baseline after a successful call).
func (b *Builder) BuildDifferential(baseline *ContextBaseline) (string, map[string]string) {
	// Collect ALL section contents (static + dynamic) for diffing.
	current := make(map[string]string)
	for _, s := range b.sections {
		current[s.Name] = s.Content
	}

	diff := baseline.Diff(current)

	if diff.IsFirst {
		// First turn: include everything (same as Build).
		return b.Build(), current
	}

	// Build a set of changed section names.
	changedSet := make(map[string]bool, len(diff.Changed))
	for _, name := range diff.Changed {
		changedSet[name] = true
	}

	var parts []string
	omittedStatic := 0
	omittedDynamic := 0

	// Static sections — include only changed ones.
	for _, s := range b.sections {
		if !s.Static {
			continue
		}
		if changedSet[s.Name] {
			parts = append(parts, s.Content)
		} else {
			omittedStatic++
		}
	}

	if omittedStatic > 0 {
		parts = append(parts, fmt.Sprintf("[System instructions unchanged from previous turn (%d section(s) omitted)]", omittedStatic))
	}

	parts = append(parts, DynamicBoundary)

	// Dynamic sections — include only changed ones.
	for _, s := range b.sections {
		if s.Static {
			continue
		}
		if changedSet[s.Name] {
			parts = append(parts, s.Content)
		} else {
			omittedDynamic++
		}
	}

	if omittedDynamic > 0 {
		parts = append(parts, fmt.Sprintf("[Context: %d section(s) unchanged from previous turn, omitted to save tokens]", omittedDynamic))
	}

	return strings.Join(parts, "\n\n"), current
}

// BuildDefault builds a system prompt with default sections and project context.
// The mode parameter controls which sections are included:
//   - "build": full sections (default behavior)
//   - "plan": full sections + plan-mode workflow instructions
//   - "explore": stripped-down prompt for codebase search subagents
//
// When cachingSupported is false and a non-nil baseline is provided, uses
// differential context injection to omit unchanged dynamic sections.
func BuildDefault(ctx *ProjectContext, mode string, cachingSupported bool, baseline *ContextBaseline) string {
	b := NewBuilder()

	if mode == "explore" {
		// Explore subagents get a lean, focused prompt.
		b.AddStaticSection(SectionExploreMode, ExploreSection())
		b.AddDynamicSection(SectionFilesystem, FilesystemSection(ctx.AllowedDirs))
		b.AddDynamicSection(SectionEnvironment, EnvironmentSection(ctx))
	} else {
		// Build and plan modes share the full section set.
		b.AddStaticSection(SectionIntro, IntroSection())

		// Personality/identity section (after intro, before system).
		personalityContent := PersonalitySection(LoadSOUL(ctx.ProjectRoot), ctx.Personality)
		if personalityContent != "" {
			b.AddStaticSection(SectionPersonality, personalityContent)
		}

		b.AddStaticSection(SectionSystem, SystemSection())
		b.AddStaticSection(SectionTasks, TasksSection())
		b.AddStaticSection(SectionActions, ActionsSection())
		b.AddStaticSection(SectionBuiltinSkills, BuiltinSkillsSection())

		b.AddDynamicSection(SectionFilesystem, FilesystemSection(ctx.AllowedDirs))
		b.AddDynamicSection(SectionEnvironment, EnvironmentSection(ctx))
		b.AddDynamicSection(SectionProject, ProjectSection(ctx))
		b.AddDynamicSection(SectionGit, GitSection(ctx))
		b.AddDynamicSection(SectionInstructions, InstructionsSection(ctx.ContextFiles))
		b.AddDynamicSection(SectionMemory, MemoriesSection(ctx.Memories))

		// L1 working memory: inject active topic as a focus signal.
		if ctx.ActiveTopic != "" {
			b.AddDynamicSection(SectionActiveTopic, "[Active Topic: "+ctx.ActiveTopic+"]")
		}

		// Runtime diagnostics: degraded tools, context health alerts.
		// Only injected when there are actionable issues (zero tokens when healthy).
		b.AddDynamicSection(SectionDiagnostics, DiagnosticsSection(ctx.Diagnostics))

		if mode == "plan" {
			b.AddDynamicSection(SectionPlanMode, PlanModeSection())
		}
	}

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
