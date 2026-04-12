package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/qiangli/ycode/internal/commands"
)

// completionMaxVisible is the maximum number of suggestions shown at once.
const completionMaxVisible = 8

// completionItem represents a single slash command suggestion.
type completionItem struct {
	Name        string // command name without leading /
	Description string
	IsSkill     bool // true for skill commands (vs built-in)
}

// completionState tracks the slash command completion popup.
type completionState struct {
	items    []completionItem // filtered suggestions
	selected int              // index of highlighted item (-1 = none)
	visible  bool             // whether the popup is showing
	scroll   int              // scroll offset for long lists
}

// buildCompletionItems gathers all completable slash commands from the registry and skills directory.
func buildCompletionItems(registry *commands.Registry, workDir string) []completionItem {
	var items []completionItem

	// Built-in commands from the registry.
	for _, spec := range registry.List() {
		items = append(items, completionItem{
			Name:        spec.Name,
			Description: spec.Description,
		})
	}

	// Add quit/exit which are handled inline, not in the registry.
	items = append(items,
		completionItem{Name: "quit", Description: "Exit ycode"},
		completionItem{Name: "exit", Description: "Exit ycode"},
	)

	// Discover skills from skills/*/skill.md in the working directory.
	skillsDir := filepath.Join(workDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillFile := filepath.Join(skillsDir, e.Name(), "skill.md")
			if _, err := os.Stat(skillFile); err == nil {
				// Only add if not already a registered command.
				name := e.Name()
				if !hasItem(items, name) {
					items = append(items, completionItem{
						Name:        name,
						Description: "skill",
						IsSkill:     true,
					})
				}
			}
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func hasItem(items []completionItem, name string) bool {
	for _, it := range items {
		if it.Name == name {
			return true
		}
	}
	return false
}

// filterCompletions returns items matching the given prefix (case-insensitive).
func filterCompletions(all []completionItem, prefix string) []completionItem {
	if prefix == "" {
		return all
	}
	lower := strings.ToLower(prefix)
	var matched []completionItem
	for _, item := range all {
		if strings.HasPrefix(strings.ToLower(item.Name), lower) {
			matched = append(matched, item)
		}
	}
	return matched
}

// updateCompletion recalculates completion state based on current textarea input.
func (cs *completionState) update(all []completionItem, input string) {
	input = strings.TrimLeft(input, " \t")

	// Only show completions when input is "/" or "/partial" (no space after command name).
	if !strings.HasPrefix(input, "/") || strings.Contains(input[1:], " ") {
		cs.visible = false
		cs.items = nil
		cs.selected = -1
		cs.scroll = 0
		return
	}

	prefix := input[1:] // strip leading /
	cs.items = filterCompletions(all, prefix)
	cs.visible = len(cs.items) > 0
	if cs.selected >= len(cs.items) {
		cs.selected = len(cs.items) - 1
	}
	if cs.selected < 0 && len(cs.items) > 0 {
		cs.selected = 0
	}
	cs.clampScroll()
}

// moveUp moves the selection up.
func (cs *completionState) moveUp() {
	if len(cs.items) == 0 {
		return
	}
	cs.selected--
	if cs.selected < 0 {
		cs.selected = len(cs.items) - 1
	}
	cs.clampScroll()
}

// moveDown moves the selection down.
func (cs *completionState) moveDown() {
	if len(cs.items) == 0 {
		return
	}
	cs.selected++
	if cs.selected >= len(cs.items) {
		cs.selected = 0
	}
	cs.clampScroll()
}

// clampScroll ensures the selected item is visible in the scroll window.
func (cs *completionState) clampScroll() {
	if cs.selected < cs.scroll {
		cs.scroll = cs.selected
	}
	if cs.selected >= cs.scroll+completionMaxVisible {
		cs.scroll = cs.selected - completionMaxVisible + 1
	}
}

// selectedName returns the name of the currently selected item, or empty.
func (cs *completionState) selectedName() string {
	if cs.selected >= 0 && cs.selected < len(cs.items) {
		return cs.items[cs.selected].Name
	}
	return ""
}

// dismiss hides the completion popup.
func (cs *completionState) dismiss() {
	cs.visible = false
	cs.items = nil
	cs.selected = -1
	cs.scroll = 0
}

// renderCompletion renders the completion popup as a styled string.
func renderCompletion(cs *completionState, width int) string {
	if !cs.visible || len(cs.items) == 0 {
		return ""
	}

	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).Bold(true)        // purple
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))                   // dim
	selNameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Bold(true)     // black on highlight
	selDescStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#404040"))                // dark on highlight
	selBgStyle := lipgloss.NewStyle().Background(lipgloss.Color("#a78bfa"))                  // purple bg for selected row
	skillTag := lipgloss.NewStyle().Foreground(lipgloss.Color("#f97316")).SetString("skill") // orange

	// Determine visible window.
	end := cs.scroll + completionMaxVisible
	if end > len(cs.items) {
		end = len(cs.items)
	}
	visible := cs.items[cs.scroll:end]

	// Find max name width for alignment.
	maxName := 0
	for _, item := range visible {
		if len(item.Name) > maxName {
			maxName = len(item.Name)
		}
	}

	var lines []string
	for i, item := range visible {
		idx := cs.scroll + i
		pad := strings.Repeat(" ", maxName-len(item.Name))

		desc := item.Description
		if item.IsSkill {
			desc = skillTag.String()
		}

		if idx == cs.selected {
			line := fmt.Sprintf(" /%s%s  %s ", selNameStyle.Render(item.Name), pad, selDescStyle.Render(desc))
			line = selBgStyle.Render(line)
			// Pad to full width.
			lineWidth := lipgloss.Width(line)
			if lineWidth < width {
				line += selBgStyle.Render(strings.Repeat(" ", width-lineWidth))
			}
			lines = append(lines, line)
		} else {
			line := fmt.Sprintf(" /%s%s  %s", nameStyle.Render(item.Name), pad, descStyle.Render(desc))
			lines = append(lines, line)
		}
	}

	// Scroll indicators.
	if cs.scroll > 0 {
		indicator := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373")).Render("  ↑ more")
		lines = append([]string{indicator}, lines...)
	}
	if end < len(cs.items) {
		indicator := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373")).Render(
			fmt.Sprintf("  ↓ %d more", len(cs.items)-end))
		lines = append(lines, indicator)
	}

	return strings.Join(lines, "\n")
}
