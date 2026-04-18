package cli

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

const appTitle = "ycode"

// alertDoneMsg is sent when a task completes to trigger visual alerts.
type alertDoneMsg struct{}

// alertDone sends an alertDoneMsg through bubbletea's event loop.
func alertDone() tea.Cmd {
	return func() tea.Msg { return alertDoneMsg{} }
}

// handleAlertDone processes the alert by setting the window title and ringing the bell.
// Must be called from Update with access to the TUIModel (and m.program).
func (m *TUIModel) handleAlertDone() tea.Cmd {
	if m.program != nil {
		m.program.SetWindowTitle("✓ " + appTitle + " — Done")
	}
	// BEL to trigger macOS dock bounce.
	fmt.Fprint(os.Stderr, "\a")
	return nil
}

// resetTitle restores the terminal window title to the default.
func (m *TUIModel) resetTitle() {
	if m.program != nil {
		m.program.SetWindowTitle(appTitle)
	}
}
