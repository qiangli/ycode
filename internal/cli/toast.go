package cli

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ToastLevel indicates the severity of a toast notification.
type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastSuccess
	ToastWarning
	ToastError
)

// toastMessage represents a single toast notification.
type toastMessage struct {
	Text      string
	Level     ToastLevel
	CreatedAt time.Time
	Duration  time.Duration
}

// toastState manages the stack of active toast notifications.
type toastState struct {
	messages []toastMessage
}

const defaultToastDuration = 4 * time.Second

// add pushes a new toast notification.
func (ts *toastState) add(text string, level ToastLevel) {
	ts.messages = append(ts.messages, toastMessage{
		Text:      text,
		Level:     level,
		CreatedAt: time.Now(),
		Duration:  defaultToastDuration,
	})
}

// prune removes expired toasts. Returns true if any were removed.
func (ts *toastState) prune() bool {
	now := time.Now()
	n := 0
	for _, msg := range ts.messages {
		if now.Sub(msg.CreatedAt) < msg.Duration {
			ts.messages[n] = msg
			n++
		}
	}
	changed := n < len(ts.messages)
	ts.messages = ts.messages[:n]
	return changed
}

// hasActive returns true if there are visible toasts.
func (ts *toastState) hasActive() bool {
	return len(ts.messages) > 0
}

// renderToasts renders the toast stack aligned to the right.
func renderToasts(ts *toastState, width int) string {
	if !ts.hasActive() {
		return ""
	}

	var lines []string
	// Show at most 3 toasts at a time.
	start := 0
	if len(ts.messages) > 3 {
		start = len(ts.messages) - 3
	}

	for _, msg := range ts.messages[start:] {
		var icon string
		var color lipgloss.Color

		switch msg.Level {
		case ToastSuccess:
			icon = "✓"
			color = "#34d399" // green
		case ToastWarning:
			icon = "⚠"
			color = "#fbbf24" // yellow
		case ToastError:
			icon = "✗"
			color = "#f87171" // red
		default:
			icon = "ℹ"
			color = "#60a5fa" // blue
		}

		style := lipgloss.NewStyle().
			Foreground(color).
			Bold(true)

		text := style.Render(icon+" ") + msg.Text
		lines = append(lines, text)
	}

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		Align(lipgloss.Right).
		Width(width - 4).
		PaddingRight(2)

	return boxStyle.Render(content)
}
