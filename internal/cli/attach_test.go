package cli

import (
	"strings"
	"testing"

	"github.com/qiangli/coreutils/pkg/chat"

	"github.com/qiangli/ycode/internal/runtime/session"
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

// THE REQUIREMENT: after talking to one third-party tool, switching to another
// must hand the second one everything the first said.
//
// ycode is the system of record across tools, not a pipe between them. Without
// recording, a handoff would carry a conversation that stops at the moment the
// first tool took over — losing exactly the part that made the switch worth
// making.
func TestAttachedExchangesEnterTheTranscriptAndTravelOnward(t *testing.T) {
	sess, err := session.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{session: sess, workDir: t.TempDir()}

	before := a.handoffContext()
	if strings.Contains(before, "ROUTING BUG") {
		t.Fatal("precondition: the transcript already mentions the reply")
	}

	att := &attachedSession{target: "Arlo (codex:gpt-5.5, L4)", agent: "codex-gpt-5.5"}
	a.recordAttachedExchange("why is the retry failing?", "It is a ROUTING BUG in the fallback.", att)

	// The exchange is in ycode's own transcript...
	if len(sess.Messages) != 2 {
		t.Fatalf("expected the request and the reply to be recorded, got %d messages", len(sess.Messages))
	}
	if sess.Messages[0].Role != session.RoleUser {
		t.Errorf("first recorded message role = %v, want user", sess.Messages[0].Role)
	}
	if sess.Messages[1].Role != session.RoleAssistant {
		t.Errorf("second recorded message role = %v, want assistant", sess.Messages[1].Role)
	}

	// ...attributed, so it does not read as though ycode said it...
	replyText := sess.Messages[1].Content[0].Text
	if !strings.Contains(replyText, "Arlo") {
		t.Errorf("reply is not attributed to the agent that produced it: %q", replyText)
	}
	if sess.Messages[1].Model == "" {
		t.Error("reply carries no Model attribution")
	}

	// ...and it travels to whatever tool is switched to next.
	after := a.handoffContext()
	if !strings.Contains(after, "ROUTING BUG") {
		t.Error("the next tool would not receive what the previous one said")
	}
	if !strings.Contains(after, "why is the retry failing?") {
		t.Error("the next tool would not receive the request either")
	}
}

// What is STORED must be clean data, even though what is DISPLAYED keeps
// colour: this text is replayed as prompt context, where escape sequences are
// noise to a model and a prompt-injection surface.
func TestRecordedContextIsSanitizedNotColoured(t *testing.T) {
	sess, err := session.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{session: sess, workDir: t.TempDir()}
	att := &attachedSession{target: "Arlo", agent: "codex-gpt-5.5"}

	a.recordAttachedExchange("q", chat.SanitizeTurn("\x1b[31mred answer\x1b[0m"), att)

	stored := sess.Messages[1].Content[0].Text
	if strings.Contains(stored, "\x1b") {
		t.Errorf("escape sequences were stored as context: %q", stored)
	}
	if !strings.Contains(stored, "red answer") {
		t.Errorf("the answer itself was lost: %q", stored)
	}
}
