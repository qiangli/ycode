package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/qiangli/coreutils/pkg/chat"
	"github.com/qiangli/coreutils/pkg/handoff"

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

// The status bar makes a CLAIM about where replies are coming from. If it
// says ycode's model while another agent is answering, it is lying about
// authorship — so both states are pinned.
func TestStatusBarNamesTheAnsweringAgentAndTheModelUnderneath(t *testing.T) {
	a := &App{}

	if got := a.AttachedBinding(); got != "" {
		t.Errorf("detached AttachedBinding() = %q, want empty so the bar shows ycode's own model", got)
	}

	a.attached = &attachedSession{
		target:    "Arlo (codex:gpt-5.5, L4)",
		agent:     "codex-gpt-5.5",
		matrixKey: "codex:gpt-5.5",
	}
	if got := a.AttachedBinding(); got != "codex:gpt-5.5" {
		t.Errorf("AttachedBinding() = %q, want the tool:model that is actually answering", got)
	}
	if got := a.AttachedLabel(); got == "" {
		t.Error("AttachedLabel() is empty while attached")
	}

	// A nickname alone would not say which MODEL is producing the text.
	a.attached.matrixKey = ""
	if got := a.AttachedBinding(); got != "codex-gpt-5.5" {
		t.Errorf("with no binding, AttachedBinding() = %q, want the agent name as fallback", got)
	}
}

// Takeover cannot capture what happened — the child owned the terminal, and
// the capture wrapper that would have recorded it is the same thing that
// mangles a nested TUI's rendering.
//
// So the gap must be VISIBLE. Without a marker the transcript runs straight
// from before the handover to after it, and the next agent reads a continuous
// conversation that never happened. An absence it is TOLD about is
// recoverable — it can ask; silence is not.
func TestTakeoverRecordsTheGapItCannotCapture(t *testing.T) {
	sess, err := session.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{session: sess, workDir: t.TempDir()}

	a.recordTakeoverGap("Beatrix (claude:opus4.8, L3)", 4*time.Minute+12*time.Second, 0)

	if len(sess.Messages) != 1 {
		t.Fatalf("expected the handover to be recorded, got %d messages", len(sess.Messages))
	}
	text := sess.Messages[0].Content[0].Text
	for _, want := range []string{"handover", "Beatrix", "NOT captured", "Ask the user"} {
		if !strings.Contains(text, want) {
			t.Errorf("marker does not convey %q: %q", want, text)
		}
	}

	// And it reaches the next tool.
	if !strings.Contains(a.handoffContext(), "NOT captured") {
		t.Error("the next agent would not be told the transcript has a gap")
	}
}

// Handoff goes through BASHY, not a private note file. bashy handoff already
// solves this for every tool — brief, next action AND the in-flight diff, into
// a durable record that travels and that `bashy resume` picks up cold, in a
// different tool, on a different machine.
func TestHandoffInstructionDirectsToBashy(t *testing.T) {
	out := appendHandoffInstruction("prior conversation")
	if !strings.Contains(out, "prior conversation") {
		t.Error("the carried context was dropped")
	}
	if !strings.Contains(out, "bashy handoff") {
		t.Error("the instruction does not name `bashy handoff`")
	}
	if !strings.Contains(out, "--next") {
		t.Error("the instruction does not ask for a next action")
	}
	if !strings.Contains(out, "BEFORE YOU EXIT") {
		t.Error("the instruction does not say when to hand off")
	}
	// The old ad-hoc mechanism taught a ycode-shaped habit no other tool shares.
	if strings.Contains(out, "note file") && !strings.Contains(out, "Do not write a private note file") {
		t.Error("the instruction should steer AWAY from private note files")
	}
}

// `bashy resume` will happily return a handoff from days ago. Recording that as
// though the agent had just written it would MANUFACTURE evidence — the exact
// false positive the provenance design exists to prevent. Only ids absent
// before the switch count.
func TestOnlyHandoffsWrittenDuringTheSwitchCount(t *testing.T) {
	before := handoffSeen()

	// Nothing new has been written, so nothing may be attributed to this switch
	// — even though pre-existing records almost certainly exist on this host.
	if rec := newHandoffSince(before); rec != nil {
		t.Errorf("a pre-existing handoff was attributed to this switch: %s", rec.ID)
	}

	// And the snapshot is not vacuously empty on a host that has records.
	if all, err := handoff.List(handoff.DefaultDir()); err == nil && len(all) > 0 && len(before) == 0 {
		t.Error("handoffSeen() returned nothing while records exist — every one would look new")
	}
}

func TestSummarizeHandoffCarriesBriefNextAndBlockers(t *testing.T) {
	rec := &handoff.Record{
		ID:         "20260725T000000Z-abc",
		Continuity: "Fixed the retry budget.",
		NextAction: "Run the gate.",
		Blockers:   []string{"waiting on a key"},
	}
	rec.Work.Repo = "/tmp/repo"

	got := summarizeHandoff(rec)
	for _, want := range []string{"Fixed the retry budget.", "Next: Run the gate.", "Blocked: waiting on a key"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %q", want, got)
		}
	}
	// The in-flight diff cannot be inlined, but the reader must know it exists
	// and how to apply it.
	if !strings.Contains(got, "bashy resume 20260725T000000Z-abc") {
		t.Errorf("summary does not tell the reader how to recover the in-flight work: %q", got)
	}
}

// Provenance must be stated, not implied. A scrape read as though it were a
// verbatim record invites confident conclusions drawn from text that was never
// quite what was said — and a reader cannot discount what it does not know is
// uncertain.
func TestScrapeFallbackIsLabelledAsAReconstruction(t *testing.T) {
	sess, err := session.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{session: sess, workDir: t.TempDir()}

	a.recordScrapeFallback("Arlo (codex:gpt-5.5, L4)", 2*time.Minute, 3)

	if len(sess.Messages) != 1 {
		t.Fatalf("expected the provenance note, got %d messages", len(sess.Messages))
	}
	text := sess.Messages[0].Content[0].Text
	for _, want := range []string{"NO handoff note", "reconstruction", "indicative rather than verbatim"} {
		if !strings.Contains(text, want) {
			t.Errorf("provenance note does not convey %q: %q", want, text)
		}
	}

	// With no turns there is nothing to qualify, so nothing is said.
	sess2, _ := session.New(t.TempDir())
	b := &App{session: sess2}
	b.recordScrapeFallback("Arlo", time.Second, 0)
	if len(sess2.Messages) != 0 {
		t.Error("a session with no turns should produce no provenance note")
	}
}
