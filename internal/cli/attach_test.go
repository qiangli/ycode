package cli

import (
	"strings"
	"testing"
)

// Colour must SURVIVE. The other agent's output should look the way it looks;
// stripping SGR (which chat.SanitizeTurn does, correctly, for text destined to
// be stored or replayed as data) would hand the user a colourless wall.
func TestRenderKeepsColour(t *testing.T) {
	in := "\x1b[31mred\x1b[0m and \x1b[1mbold\x1b[22m"
	got := renderAgentOutput(in)
	for _, want := range []string{"\x1b[31m", "\x1b[0m", "\x1b[1m", "red", "bold"} {
		if !strings.Contains(got, want) {
			t.Errorf("render dropped %q from %q: %q", want, in, got)
		}
	}
}

// Screen control must NOT survive. ycode owns the screen: an absolute cursor
// move or an erase-display from the child would scribble outside ycode's
// layout and corrupt both renderings.
func TestRenderStripsScreenControl(t *testing.T) {
	cases := map[string]string{
		"cursor home":    "\x1b[H",
		"cursor address": "\x1b[12;40H",
		"erase display":  "\x1b[2J",
		"erase line":     "\x1b[K",
		"scroll region":  "\x1b[1;24r",
		"alt screen on":  "\x1b[?1049h",
		"alt screen off": "\x1b[?1049l",
		"cursor up":      "\x1b[3A",
		"save cursor":    "\x1b7",
		"restore cursor": "\x1b8",
		"charset":        "\x1b(B",
		"osc title":      "\x1b]0;some title\x07",
	}
	for name, seq := range cases {
		got := renderAgentOutput("before" + seq + "after")
		if strings.Contains(got, seq) {
			t.Errorf("%s survived rendering: %q", name, got)
		}
		if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
			t.Errorf("%s ate surrounding text: %q", name, got)
		}
	}
}

// A spinner draws frames with \r + redraw, so the capture holds every frame
// while only the last was ever visible. Rendering them all would scroll a
// column of garbage through the viewport.
func TestRenderCollapsesCarriageRepaint(t *testing.T) {
	got := renderAgentOutput("⠋ thinking\r⠙ thinking\r⠹ thinking\rDone.")
	if strings.Contains(got, "thinking") {
		t.Errorf("intermediate spinner frames survived: %q", got)
	}
	if !strings.Contains(got, "Done.") {
		t.Errorf("final frame was lost: %q", got)
	}
}

func TestRenderDropsControlCharsButKeepsLayout(t *testing.T) {
	got := renderAgentOutput("line one\nline\ttwo\x00\x07\n")
	if !strings.Contains(got, "line one\nline\ttwo") {
		t.Errorf("newline or tab was lost: %q", got)
	}
	if strings.ContainsAny(got, "\x00\x07") {
		t.Errorf("control characters survived: %q", got)
	}
}

// The regression this whole filter exists to prevent: the tail of a claude
// turn arriving as literal text because the private-mode CSI markers were not
// matched. chat's own sanitizer comment records this happening.
func TestRenderHandlesPrivateModeResetSequences(t *testing.T) {
	got := renderAgentOutput("answer\x1b[>4m\x1b[<u")
	if strings.Contains(got, "[>4m") || strings.Contains(got, "[<u") {
		t.Errorf("private-mode reset leaked as literal text: %q", got)
	}
	if !strings.Contains(got, "answer") {
		t.Errorf("answer was lost: %q", got)
	}
}

func TestAttachedLabelEmptyWhenDetached(t *testing.T) {
	a := &App{}
	if got := a.AttachedLabel(); got != "" {
		t.Errorf("AttachedLabel() = %q on a detached app, want empty", got)
	}
	if _, err := a.Detach(); err == nil {
		t.Error("Detach() on a detached app should error")
	}
	if _, err := a.Forward(t.Context(), "hello"); err == nil {
		t.Error("Forward() with nothing attached should error")
	}
}
