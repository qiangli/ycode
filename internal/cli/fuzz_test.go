package cli

import (
	"encoding/json"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// FuzzTUIUpdate feeds random key sequences into an initialized TUIModel.
// The model must never panic in Update() or View().
func FuzzTUIUpdate(f *testing.F) {
	// Seed corpus with known key sequences.
	f.Add([]byte{0x03})              // Ctrl+C
	f.Add([]byte{0x04})              // Ctrl+D
	f.Add([]byte{0x0d})              // Enter
	f.Add([]byte("/help\r"))         // slash command
	f.Add([]byte("/quit\r"))         // quit
	f.Add([]byte("!ls\r"))           // shell command
	f.Add([]byte("/"))               // trigger completion
	f.Add([]byte{0x0b})              // Ctrl+K (command palette)
	f.Add([]byte{0x1b})              // Escape
	f.Add([]byte("hello world\r"))   // regular prompt
	f.Add([]byte("/btw question\r")) // side query

	f.Fuzz(func(t *testing.T, data []byte) {
		m := newTestTUIModel(t)
		var model tea.Model = m

		for _, b := range data {
			var msg tea.Msg
			switch {
			case b == 0x03:
				msg = tea.KeyMsg{Type: tea.KeyCtrlC}
			case b == 0x04:
				msg = tea.KeyMsg{Type: tea.KeyCtrlD}
			case b == 0x0d:
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case b == 0x09:
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case b == 0x1b:
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			case b == 0x0b:
				msg = tea.KeyMsg{Type: tea.KeyCtrlK}
			case b < 32:
				// Map other control chars to runes to avoid unsupported key types.
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(b + 0x40)}}
			default:
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(b)}}
			}

			var cmd tea.Cmd
			model, cmd = model.Update(msg)

			// If the model returns tea.Quit, reset to a fresh model
			// so the fuzzer can keep exploring.
			if cmd != nil {
				result := cmd()
				if _, ok := result.(tea.QuitMsg); ok {
					m = newTestTUIModel(t)
					model = m
				}
			}
		}

		// Call View to ensure no panic during rendering.
		_ = model.View()
	})
}

// FuzzToolDetail fuzzes the toolDetail function with random tool names and JSON input.
func FuzzToolDetail(f *testing.F) {
	f.Add("bash", []byte(`{"command":"ls"}`))
	f.Add("read_file", []byte(`{"file_path":"/foo.go"}`))
	f.Add("write_file", []byte(`{"file_path":"/bar.go"}`))
	f.Add("edit_file", []byte(`{}`))
	f.Add("glob_search", []byte(`{"pattern":"*.go"}`))
	f.Add("grep_search", []byte(`{"pattern":"TODO"}`))
	f.Add("WebFetch", []byte(`{"url":"https://example.com"}`))
	f.Add("WebSearch", []byte(`{"query":"test"}`))
	f.Add("Agent", []byte(`{"description":"explore"}`))
	f.Add("unknown", []byte(`not json at all`))
	f.Add("", []byte(`{}`))
	f.Add("bash", []byte{})

	f.Fuzz(func(t *testing.T, name string, input []byte) {
		// Must not panic.
		_ = toolDetail(name, json.RawMessage(input))
	})
}
