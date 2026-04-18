package cli

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

const appTitle = "ycode"

// alertDone returns tea commands that notify the user a task has completed:
// - sets the terminal window title to "✓ ycode - Done"
// - sends a BEL character to trigger a macOS dock bounce (when the terminal is unfocused)
func alertDone() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("✓ "+appTitle+" — Done"),
		func() tea.Msg {
			fmt.Fprint(os.Stderr, "\a")
			return nil
		},
	)
}

// alertReset restores the terminal window title to the default.
func alertReset() tea.Cmd {
	return tea.SetWindowTitle(appTitle)
}
