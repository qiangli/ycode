package cli

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// logEntryMsg is a bubbletea message carrying a formatted log line
// for display in the TUI viewport.
type logEntryMsg struct {
	text string
}

// tuiLogHandler is an slog.Handler that formats log records with elapsed time
// and routes them through the bubbletea program for display in the viewport.
//
// Format:
//
//	[+1.2s] routing decision                     (info/debug)
//	[+1.2s] ✗ classification failed: timeout     (warn/error)
type tuiLogHandler struct {
	start    time.Time
	program  *tea.Program
	attrs    []slog.Attr
	group    string
	minLevel slog.Level
}

// newTUILogHandler creates a handler that sends formatted log entries
// to the bubbletea program as logEntryMsg messages.
func newTUILogHandler(program *tea.Program, minLevel slog.Level) *tuiLogHandler {
	return &tuiLogHandler{
		start:    time.Now(),
		program:  program,
		minLevel: minLevel,
	}
}

var (
	logElapsedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#737373")) // dim gray
	logErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")) // red
	logWarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#fbbf24")) // yellow
	logInfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#a3a3a3")) // neutral gray
)

func (h *tuiLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *tuiLogHandler) Handle(_ context.Context, r slog.Record) error {
	elapsed := r.Time.Sub(h.start)
	elapsedStr := formatElapsed(elapsed)

	// Build the message with key error attributes inline.
	msg := r.Message
	var errStr string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "error" || a.Key == "err" {
			errStr = a.Value.String()
		}
		return true
	})
	if errStr != "" {
		msg = msg + ": " + errStr
	}

	// Format based on level.
	var line string
	prefix := logElapsedStyle.Render(fmt.Sprintf("[+%s]", elapsedStr))
	switch {
	case r.Level >= slog.LevelError:
		line = fmt.Sprintf("%s %s %s", prefix, logErrorStyle.Render("✗"), logErrorStyle.Render(msg))
	case r.Level >= slog.LevelWarn:
		line = fmt.Sprintf("%s %s %s", prefix, logWarnStyle.Render("⚠"), logWarnStyle.Render(msg))
	default:
		line = fmt.Sprintf("%s %s", prefix, logInfoStyle.Render(msg))
	}

	h.program.Send(logEntryMsg{text: line})
	return nil
}

func (h *tuiLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &h2
}

func (h *tuiLogHandler) WithGroup(name string) slog.Handler {
	h2 := *h
	if h2.group != "" {
		h2.group += "." + name
	} else {
		h2.group = name
	}
	return &h2
}

// formatElapsed renders a duration as a compact human-readable string:
//
//	< 1s  → "0.3s"
//	< 60s → "12.4s"
//	< 60m → "2m31s"
//	else  → "1h5m"
func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", h, m)
	}
}
