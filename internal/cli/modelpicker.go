package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/qiangli/ycode/internal/api"
)

// modelPickerMaxVisible is the max items shown at once.
const modelPickerMaxVisible = 12

// modelPickerItem represents a model in the picker.
type modelPickerItem struct {
	ID       string // full model ID or alias
	Alias    string // short alias if any
	Provider string // provider kind
	Current  bool   // true if this is the active model
}

// modelPickerState tracks the model picker overlay.
type modelPickerState struct {
	items    []modelPickerItem
	filtered []modelPickerItem
	filter   string
	selected int
	scroll   int
	visible  bool
}

// buildModelPickerItems creates the list of available models.
func buildModelPickerItems(currentModel string) []modelPickerItem {
	var items []modelPickerItem

	// Add aliased models first (most commonly used).
	for alias, fullID := range api.ModelAliases {
		provider := detectProviderFromModel(fullID)
		items = append(items, modelPickerItem{
			ID:       fullID,
			Alias:    alias,
			Provider: provider,
			Current:  fullID == currentModel || alias == currentModel,
		})
	}

	return items
}

// detectProviderFromModel guesses provider from model name.
func detectProviderFromModel(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.HasPrefix(lower, "claude-"):
		return "anthropic"
	case strings.HasPrefix(lower, "gpt-") || strings.HasPrefix(lower, "o1-") || strings.HasPrefix(lower, "o3-"):
		return "openai"
	case strings.HasPrefix(lower, "gemini-"):
		return "gemini"
	case strings.HasPrefix(lower, "grok"):
		return "xai"
	case strings.HasPrefix(lower, "qwen"):
		return "dashscope"
	case strings.HasPrefix(lower, "kimi") || strings.HasPrefix(lower, "moonshot"):
		return "moonshot"
	default:
		return "unknown"
	}
}

func (mp *modelPickerState) open(currentModel string) {
	mp.items = buildModelPickerItems(currentModel)
	mp.filter = ""
	mp.applyFilter()
	mp.visible = true
	// Select current model.
	for i, item := range mp.filtered {
		if item.Current {
			mp.selected = i
			break
		}
	}
	mp.clampScroll()
}

func (mp *modelPickerState) close() {
	mp.visible = false
	mp.filter = ""
	mp.filtered = nil
	mp.selected = 0
	mp.scroll = 0
}

func (mp *modelPickerState) applyFilter() {
	if mp.filter == "" {
		mp.filtered = mp.items
	} else {
		lower := strings.ToLower(mp.filter)
		mp.filtered = nil
		for _, item := range mp.items {
			if strings.Contains(strings.ToLower(item.ID), lower) ||
				strings.Contains(strings.ToLower(item.Alias), lower) ||
				strings.Contains(strings.ToLower(item.Provider), lower) {
				mp.filtered = append(mp.filtered, item)
			}
		}
	}
	if mp.selected >= len(mp.filtered) {
		mp.selected = len(mp.filtered) - 1
	}
	if mp.selected < 0 && len(mp.filtered) > 0 {
		mp.selected = 0
	}
	mp.clampScroll()
}

func (mp *modelPickerState) typeChar(ch rune) {
	mp.filter += string(ch)
	mp.applyFilter()
}

func (mp *modelPickerState) backspace() {
	if len(mp.filter) > 0 {
		mp.filter = mp.filter[:len(mp.filter)-1]
		mp.applyFilter()
	}
}

func (mp *modelPickerState) moveUp() {
	if len(mp.filtered) == 0 {
		return
	}
	mp.selected--
	if mp.selected < 0 {
		mp.selected = len(mp.filtered) - 1
	}
	mp.clampScroll()
}

func (mp *modelPickerState) moveDown() {
	if len(mp.filtered) == 0 {
		return
	}
	mp.selected++
	if mp.selected >= len(mp.filtered) {
		mp.selected = 0
	}
	mp.clampScroll()
}

func (mp *modelPickerState) clampScroll() {
	if mp.selected < mp.scroll {
		mp.scroll = mp.selected
	}
	if mp.selected >= mp.scroll+modelPickerMaxVisible {
		mp.scroll = mp.selected - modelPickerMaxVisible + 1
	}
}

func (mp *modelPickerState) selectedModel() string {
	if mp.selected >= 0 && mp.selected < len(mp.filtered) {
		item := mp.filtered[mp.selected]
		if item.Alias != "" {
			return item.Alias
		}
		return item.ID
	}
	return ""
}

// renderModelPicker renders the model picker overlay.
func renderModelPicker(mp *modelPickerState, width, height int) string {
	if !mp.visible || len(mp.filtered) == 0 {
		return ""
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#818cf8"))
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2e8f0"))
	aliasStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).Bold(true)
	providerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))
	currentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34d399"))
	selBg := lipgloss.NewStyle().Background(lipgloss.Color("#3b3b5c"))

	var lines []string

	// Title.
	title := titleStyle.Render("  Switch Model (Ctrl+M)")
	lines = append(lines, title)

	// Filter input.
	filterLine := filterStyle.Render(fmt.Sprintf("  > %s▏", mp.filter))
	lines = append(lines, filterLine)
	lines = append(lines, "")

	// Visible items.
	end := mp.scroll + modelPickerMaxVisible
	if end > len(mp.filtered) {
		end = len(mp.filtered)
	}
	visible := mp.filtered[mp.scroll:end]

	for i, item := range visible {
		idx := mp.scroll + i
		marker := "  "
		if item.Current {
			marker = currentStyle.Render("● ")
		}

		alias := ""
		if item.Alias != "" {
			alias = aliasStyle.Render(item.Alias) + " → "
		}
		name := nameStyle.Render(item.ID)
		prov := providerStyle.Render(fmt.Sprintf(" (%s)", item.Provider))

		line := fmt.Sprintf("  %s%s%s%s", marker, alias, name, prov)

		if idx == mp.selected {
			line = selBg.Render(line)
			padWidth := width - lipgloss.Width(line) - 4
			if padWidth > 0 {
				line += selBg.Render(strings.Repeat(" ", padWidth))
			}
		}
		lines = append(lines, line)
	}

	// Scroll indicators.
	if mp.scroll > 0 {
		lines = append(lines, providerStyle.Render("  ↑ more"))
	}
	if end < len(mp.filtered) {
		lines = append(lines, providerStyle.Render(fmt.Sprintf("  ↓ %d more", len(mp.filtered)-end)))
	}

	lines = append(lines, "")
	lines = append(lines, providerStyle.Render("  Enter: select | Esc: close | Type to filter"))

	content := strings.Join(lines, "\n")

	// Box styling.
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#818cf8")).
		Padding(0, 1).
		Width(width - 10)

	return boxStyle.Render(content)
}
