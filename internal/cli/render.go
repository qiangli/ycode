package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
)

// Renderer handles markdown and syntax-highlighted output.
type Renderer struct {
	glamour *glamour.TermRenderer
}

// NewRenderer creates a new renderer with a given style.
func NewRenderer(style string) (*Renderer, error) {
	if style == "" {
		style = "dark"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(120),
	)
	if err != nil {
		return nil, fmt.Errorf("create renderer: %w", err)
	}
	return &Renderer{glamour: r}, nil
}

// RenderMarkdown renders markdown text to styled terminal output.
func (r *Renderer) RenderMarkdown(md string) (string, error) {
	out, err := r.glamour.Render(md)
	if err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// RenderPlain returns text as-is (no formatting).
func (r *Renderer) RenderPlain(text string) string {
	return text
}
