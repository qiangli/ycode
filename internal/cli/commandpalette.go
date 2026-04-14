package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// commandPaletteMaxVisible is the max items shown at once.
const commandPaletteMaxVisible = 12

// paletteItem represents an actionable command in the palette.
type paletteItem struct {
	Name        string
	Description string
	Category    string
	Keybinding  string // e.g. "ctrl+m"
}

// commandPaletteState tracks the command palette overlay.
type commandPaletteState struct {
	items    []paletteItem
	filtered []paletteItem
	filter   string
	selected int
	scroll   int
	visible  bool
}

// buildPaletteItems creates the list of all available commands and actions.
func (m *TUIModel) buildPaletteItems() []paletteItem {
	var items []paletteItem

	// Slash commands from registry.
	for _, spec := range m.app.Commands().List() {
		items = append(items, paletteItem{
			Name:        "/" + spec.Name,
			Description: spec.Description,
			Category:    spec.Category,
		})
	}

	// Built-in actions with keybindings.
	items = append(items,
		paletteItem{Name: "Switch Model", Description: "Open model picker", Keybinding: "ctrl+m", Category: "action"},
		paletteItem{Name: "Toggle Mode", Description: "Switch between build and plan mode", Keybinding: "shift+tab", Category: "action"},
		paletteItem{Name: "Cancel", Description: "Cancel current operation", Keybinding: "ctrl+c", Category: "action"},
		paletteItem{Name: "Quit", Description: "Exit ycode", Keybinding: "ctrl+d", Category: "action"},
	)

	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func (cp *commandPaletteState) open(items []paletteItem) {
	cp.items = items
	cp.filter = ""
	cp.selected = 0
	cp.scroll = 0
	cp.applyFilter()
	cp.visible = true
}

func (cp *commandPaletteState) close() {
	cp.visible = false
	cp.filter = ""
	cp.filtered = nil
	cp.selected = 0
	cp.scroll = 0
}

func (cp *commandPaletteState) applyFilter() {
	if cp.filter == "" {
		cp.filtered = cp.items
	} else {
		lower := strings.ToLower(cp.filter)
		cp.filtered = nil
		for _, item := range cp.items {
			if fuzzyMatch(strings.ToLower(item.Name), lower) ||
				fuzzyMatch(strings.ToLower(item.Description), lower) ||
				strings.Contains(strings.ToLower(item.Category), lower) {
				cp.filtered = append(cp.filtered, item)
			}
		}
	}
	if cp.selected >= len(cp.filtered) {
		cp.selected = len(cp.filtered) - 1
	}
	if cp.selected < 0 && len(cp.filtered) > 0 {
		cp.selected = 0
	}
	cp.clampScroll()
}

// fuzzyMatch returns true if all characters of pattern appear in text in order.
func fuzzyMatch(text, pattern string) bool {
	pi := 0
	for i := 0; i < len(text) && pi < len(pattern); i++ {
		if text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}

func (cp *commandPaletteState) typeChar(ch rune) {
	cp.filter += string(ch)
	cp.applyFilter()
}

func (cp *commandPaletteState) backspace() {
	if len(cp.filter) > 0 {
		cp.filter = cp.filter[:len(cp.filter)-1]
		cp.applyFilter()
	}
}

func (cp *commandPaletteState) moveUp() {
	if len(cp.filtered) == 0 {
		return
	}
	cp.selected--
	if cp.selected < 0 {
		cp.selected = len(cp.filtered) - 1
	}
	cp.clampScroll()
}

func (cp *commandPaletteState) moveDown() {
	if len(cp.filtered) == 0 {
		return
	}
	cp.selected++
	if cp.selected >= len(cp.filtered) {
		cp.selected = 0
	}
	cp.clampScroll()
}

func (cp *commandPaletteState) clampScroll() {
	if cp.selected < cp.scroll {
		cp.scroll = cp.selected
	}
	if cp.selected >= cp.scroll+commandPaletteMaxVisible {
		cp.scroll = cp.selected - commandPaletteMaxVisible + 1
	}
}

func (cp *commandPaletteState) selectedItem() *paletteItem {
	if cp.selected >= 0 && cp.selected < len(cp.filtered) {
		return &cp.filtered[cp.selected]
	}
	return nil
}

// renderCommandPalette renders the command palette overlay.
func renderCommandPalette(cp *commandPaletteState, width int) string {
	if !cp.visible || len(cp.filtered) == 0 {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8"))
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a3a3a3"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34d399"))
	catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))
	selBg := lipgloss.NewStyle().Background(lipgloss.Color("#3b3b5c"))

	var lines []string

	// Title.
	lines = append(lines, titleStyle.Render("  Command Palette (Ctrl+K)"))

	// Filter input.
	lines = append(lines, filterStyle.Render(fmt.Sprintf("  > %s▏", cp.filter)))
	lines = append(lines, "")

	// Visible items.
	end := cp.scroll + commandPaletteMaxVisible
	if end > len(cp.filtered) {
		end = len(cp.filtered)
	}
	visible := cp.filtered[cp.scroll:end]

	for i, item := range visible {
		idx := cp.scroll + i

		name := nameStyle.Render(item.Name)
		desc := descStyle.Render(" " + item.Description)
		key := ""
		if item.Keybinding != "" {
			key = keyStyle.Render(fmt.Sprintf("  [%s]", item.Keybinding))
		}

		line := fmt.Sprintf("  %s%s%s", name, desc, key)

		if idx == cp.selected {
			line = selBg.Render(line)
			padWidth := width - lipgloss.Width(line) - 4
			if padWidth > 0 {
				line += selBg.Render(strings.Repeat(" ", padWidth))
			}
		}
		lines = append(lines, line)
	}

	// Scroll indicators.
	if cp.scroll > 0 {
		lines = append(lines, catStyle.Render("  ↑ more"))
	}
	if end < len(cp.filtered) {
		lines = append(lines, catStyle.Render(fmt.Sprintf("  ↓ %d more", len(cp.filtered)-end)))
	}

	lines = append(lines, "")
	lines = append(lines, catStyle.Render("  Enter: execute | Esc: close | Type to filter"))

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#a78bfa")).
		Padding(0, 1).
		Width(width - 10)

	return boxStyle.Render(content)
}
