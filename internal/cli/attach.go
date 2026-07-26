package cli

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/qiangli/coreutils/pkg/chat"

	"github.com/qiangli/ycode/internal/agentswitch"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/session"
)

// attachQuietPeriod is how long the target must be silent before its turn is
// considered finished. It is foreman's number (pkg/foreman.quietPeriod) and it
// is deliberately the same: a pty gives us no turn boundary, so silence is the
// only signal, and it has to be sized to the slowest thing an agent legitimately
// pauses for — a long tool call, a re-prompt, a model thinking before its first
// token.
//
// It was 2s, which is shorter than the gaps a coding agent leaves WITHIN one
// answer. Every reply was therefore either cut off partway or, once the clock
// started counting from the steer, waited on for a turn that had already been
// declared over. Two seconds is how long a human is willing to wait; it is not
// how long an agent takes.
//
// What it does NOT decide is when the operator gets to read anything —
// attachStream renders the turn as it arrives. This is only how long ycode waits
// before taking the keyboard back, which is why it can afford to be generous.
const attachQuietPeriod = 25 * time.Second

// attachedSession is a live third-party agent that ycode is driving.
//
// ATTACH IS THE DEFAULT MODE, and the difference from --takeover is the whole
// point: the user never leaves the ycode TUI. Their input is intercepted here,
// forwarded to the other agent, and its reply is rendered back inside ycode.
// ycode remains the interface.
//
// That is also the only shape with a future. A terminal handover cannot travel
// over a wire, so it could never serve the web UI reached through periscope and
// outpost. A proxied session can: ycode owns the session and merely RENDERS it,
// so the render target is a TUI today and a browser later.
type attachedSession struct {
	mu        sync.Mutex
	live      *chat.Session
	target    string // human label, e.g. "Arlo (codex:gpt-5.5, L4)"
	agent     string // canonical agent name
	matrixKey string // tool:model, e.g. "codex:gpt-5.5"
	started   time.Time
	turns     int

	// seenHandoffs are the handoff ids that existed when this session began,
	// so a record written DURING it can be told from one already on disk.
	seenHandoffs map[string]bool

	// stream renders the agent's output live. Nil when nothing is listening.
	stream *attachStream
}

// attachStream renders another agent's output into ycode's viewport AS IT
// ARRIVES, instead of holding it until the turn is judged over.
//
// Without it an attached session is indistinguishable from a hung one. The turn
// boundary is a silence heuristic, so ycode must wait out the quiet period
// before it has anything to show — and for that whole time the operator faces a
// still screen with no way to tell an agent that is working from one that is
// wedged. A spinner does not resolve it either: it says ycode is waiting, which
// is true in both cases.
//
// Streaming settles it with the only evidence that actually distinguishes them,
// which is the agent's own output. It also demotes the quiet period from "how
// long until you see anything" to "how long until ycode takes the keyboard
// back" — the same 25 seconds, costing far less.
type attachStream struct {
	emit func(string)

	mu      sync.Mutex
	pending []byte
	blank   int // consecutive blank lines already emitted, carried across chunks
}

// attachStreamMaxPending bounds the wait for a newline. A tool that repaints a
// spinner or draws a box can run a long way without one, and holding its output
// forever would reintroduce exactly the silence this type exists to end.
const attachStreamMaxPending = 8 << 10

// Write buffers up to the last complete line and renders that much.
//
// The cut matters: a pty hands over arbitrary byte boundaries, and the escape
// filtering below is regex-based, so a sequence split across two writes would
// survive as garbage on screen. No escape sequence contains a newline, so a line
// boundary is always a safe place to cut — and it is also where carriage-return
// repaints (a spinner's overwritten frames) can be collapsed correctly, since
// the whole line is present to collapse.
func (s *attachStream) Write(p []byte) (int, error) {
	if s == nil {
		return len(p), nil
	}
	s.mu.Lock()
	s.pending = append(s.pending, p...)
	cut := bytes.LastIndexByte(s.pending, '\n') + 1
	if cut == 0 && len(s.pending) > attachStreamMaxPending {
		cut = len(s.pending)
	}
	var chunk string
	if cut > 0 {
		chunk = string(s.pending[:cut])
		s.pending = append(s.pending[:0], s.pending[cut:]...)
	}
	s.mu.Unlock()

	s.render(chunk)
	return len(p), nil
}

// Flush renders whatever is held back, for the end of a turn — an agent's last
// line often arrives without a trailing newline, and it is usually the answer.
func (s *attachStream) Flush() {
	if s == nil {
		return
	}
	s.mu.Lock()
	chunk := string(s.pending)
	s.pending = s.pending[:0]
	s.mu.Unlock()

	s.render(chunk)
}

// emitLine writes text the AGENT did not produce — ycode's own punctuation
// around a turn. It bypasses the sanitizer for that reason, and always ends in a
// newline so the next thing on screen starts clean.
func (s *attachStream) emitLine(text string) {
	if s == nil || s.emit == nil {
		return
	}
	s.emit(text + "\n")
}

func (s *attachStream) render(chunk string) {
	if chunk == "" || s.emit == nil {
		return
	}
	out := s.capBlankLines(sanitizeAgentChunk(chunk))
	if out != "" {
		s.emit(out)
	}
}

// maxBlankRun is how many blank lines in a row survive. One separates
// paragraphs; more is the artifact described on capBlankLines.
const maxBlankRun = 1

// capBlankLines squeezes runs of blank lines out of the live stream.
//
// A TUI redraws its ENTIRE frame for every spinner tick. Once the cursor
// movement that put those frames on top of each other is stripped — as it must
// be, since ycode owns the screen — what is left is the same box drawn dozens of
// times, and the vertical whitespace between its rows becomes dozens of blank
// lines. A measured turn that answered in one word scrolled the real answer well
// off the top of the viewport.
//
// The run counter is carried ACROSS chunks: a pty delivers a frame in several
// writes, so a per-chunk squeeze would leave one blank line per write and undo
// most of the effect.
func (s *attachStream) capBlankLines(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.SplitAfter(text, "\n")
	var b strings.Builder
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, line := range lines {
		if line == "" {
			continue // SplitAfter's empty tail
		}
		if strings.TrimSpace(line) != "" {
			s.blank = 0
			b.WriteString(line)
			continue
		}
		if s.blank < maxBlankRun {
			s.blank++
			b.WriteString("\n") // normalised: a blank line is a newline, not padding
		}
	}
	return b.String()
}

// Attach starts a live session with another agent and leaves ycode in control.
func (a *App) Attach(ctx context.Context, req commands.SwitchRequest) (string, error) {
	if err := agentswitch.Guard(); err != nil {
		return "", err
	}
	target, err := agentswitch.Resolve(agentswitch.Request{Agent: req.Agent, Tool: req.Tool})
	if err != nil {
		return "", err
	}
	if a.attached != nil {
		return "", fmt.Errorf("already attached to %s — /detach first", a.attached.target)
	}

	// A tool selector has no single agent behind it; chat.Start needs one, so
	// resolve the tool's best agent rather than guessing a model.
	agentName := target.Agent.Name
	if agentName == "" {
		return "", fmt.Errorf("/tool needs an agent to drive; name one directly (/agent <name>)")
	}

	// Attended, because an attached session is one a HUMAN is driving: every
	// message it receives was typed by the operator sitting in this TUI, and every
	// reply is rendered straight back to them. That is the same relationship
	// `bashy chat -i` has, with ycode relaying instead of the terminal, so it takes
	// the same launch: the tool's OWN approval gate stays on rather than being
	// skipped by --dangerously-skip-permissions and kin.
	//
	// Without it the launcher refuses outright — it sees an auto-approve
	// kill-switch on an uncontained host and cannot tell that anyone is watching —
	// and /agent fails on exactly the machine where it is most obviously safe.
	// ReadOnly (plan mode) is stricter and wins.
	opt := chat.SessionOptions{
		Cwd:      a.workDir,
		ReadOnly: a.InPlanMode(),
		Attended: true,
		Mode:     "ycode-attach",
	}
	if !req.Fresh {
		// The carried conversation opens the session, so the target starts
		// knowing what we were doing. This is the reason to attach through
		// ycode at all rather than running the tool yourself.
		opt.Prompt = a.handoffContext()
	}

	// The handoff note is the STANDARD protocol between agent tools, not a
	// takeover workaround. Even here, where ycode does capture each turn, the
	// agent's own account beats a terminal scrape — the scrape is a
	// reconstruction that loses whatever the agent repainted over, while the
	// note is authored by the participant that knows what it did.
	seenHandoffs := handoffSeen()
	opt.Prompt = appendHandoffInstruction(opt.Prompt)

	// Render the session live. It is wired BEFORE Start so the target's answer to
	// the carried conversation — the first and often longest thing it does, and
	// the moment an operator most wants proof that the attach worked — is visible
	// as it happens rather than after the fact.
	//
	// Only when something is actually listening. LogDelta is a no-op without a
	// registered sink, and a caller that gets neither the stream nor the returned
	// turn gets silence — worse than the batching this replaces.
	var stream *attachStream
	if a.deltaFunc != nil {
		stream = &attachStream{emit: a.LogDelta}
		opt.Stream = stream
	}

	live, err := chat.Start(ctx, agentName, opt)
	if err != nil {
		return "", fmt.Errorf("attach to %s: %w", target.Label(), err)
	}

	a.attached = &attachedSession{
		live:         live,
		stream:       stream,
		target:       target.Label(),
		agent:        agentName,
		matrixKey:    target.Agent.Binding,
		started:      time.Now(),
		seenHandoffs: seenHandoffs,
	}

	carried := "carrying this conversation"
	if req.Fresh {
		carried = "fresh context"
	} else if opt.Prompt == "" {
		carried = "nothing to carry yet"
	}
	return fmt.Sprintf("→ attached to %s (%s).\n"+
		"  Everything you type now goes to it; /detach returns to ycode.",
		target.Label(), carried), nil
}

// Forward sends one user message to the attached agent and returns its reply.
//
// Say + WaitIdle + Turn is the sanctioned way to get a REPLY out of a stream
// that has no turn boundaries in it (pkg/foreman/steer.go does the same).
func (a *App) Forward(ctx context.Context, text string) (string, error) {
	att := a.attached
	if att == nil {
		return "", fmt.Errorf("not attached to an agent")
	}
	att.mu.Lock()
	defer att.mu.Unlock()

	if !att.live.Live() {
		return "", fmt.Errorf("%s has exited; /detach to return to ycode", att.target)
	}
	if err := att.live.Say(text); err != nil {
		return "", fmt.Errorf("send to %s: %w", att.target, err)
	}
	_ = att.live.WaitIdle(ctx, attachQuietPeriod)
	att.turns++

	// The agent's last line usually arrives without a trailing newline, and it is
	// usually the answer. Push it before reading the turn.
	att.stream.Flush()

	raw := att.live.Turn()
	out := strings.TrimSpace(renderAgentOutput(raw))
	if out == "" {
		// Silence is not an answer. Say so rather than render an empty turn
		// that looks like the agent replied with nothing.
		//
		// Say WHAT was waited for. "produced no output" reads as a broken agent,
		// and for a long time it was really a broken clock — the turn was declared
		// over before the model had typed a character. Naming the window makes the
		// difference legible: an agent that truly said nothing for this long is
		// wedged or waiting on something, and neither is the operator's fault.
		return "", fmt.Errorf("%s said nothing for %s — it may be wedged or waiting on input; "+
			"send another message, or /detach to take ycode back", att.target, attachQuietPeriod)
	}

	// RECORD THE EXCHANGE IN YCODE'S OWN TRANSCRIPT.
	//
	// ycode is the system of record across tools, not merely a pipe. Without
	// this, attaching to codex and then switching to claude would hand claude
	// a conversation that stops at the moment codex took over — everything
	// codex said would be lost precisely when it was needed. Recording here is
	// what makes "switch again, keep the context" true rather than aspirational.
	//
	// Stored with chat.SanitizeTurn, NOT the display rendering: this text will
	// be replayed as prompt context, and escape sequences are noise to a model
	// (and a prompt-injection surface). Colour is for the human; data is for
	// the next agent. That is the whole reason the two paths differ.
	a.recordAttachedExchange(text, chat.SanitizeTurn(raw), att)

	// Already on screen. Returning it as well would print the whole turn a second
	// time, directly under the copy the operator has been reading for the last
	// half minute — so the caller gets a blank, and the only thing left to add is
	// the separator before the next prompt.
	if att.stream != nil {
		att.stream.emitLine("")
		return "", nil
	}
	return out, nil
}

// recordAttachedExchange appends a forwarded request and its reply to ycode's
// session, attributed to the agent that produced it.
//
// Attribution is not cosmetic: an unlabelled reply in the transcript reads as
// though YCODE said it, and the next agent to receive this context would be
// told it had already reasoned things it never saw.
func (a *App) recordAttachedExchange(request, reply string, att *attachedSession) {
	if a.session == nil {
		return
	}
	_ = a.session.AddMessage(session.ConversationMessage{
		Role:    session.RoleUser,
		Content: []session.ContentBlock{{Type: session.ContentTypeText, Text: request}},
	})
	if strings.TrimSpace(reply) == "" {
		return
	}
	_ = a.session.AddMessage(session.ConversationMessage{
		Role:  session.RoleAssistant,
		Model: att.binding(),
		Content: []session.ContentBlock{{
			Type: session.ContentTypeText,
			Text: fmt.Sprintf("[via %s]\n%s", att.target, reply),
		}},
	})
}

// binding returns the tool:model this session is driving, for the Model field
// on recorded messages.
func (s *attachedSession) binding() string {
	if s.live != nil && s.live.Nick != "" {
		return s.live.Nick
	}
	return s.agent
}

// bindingLabel renders the tool:model for the status bar, falling back to the
// agent name when the catalog gave no binding.
func (s *attachedSession) bindingLabel() string {
	if s.matrixKey != "" {
		return s.matrixKey
	}
	return s.agent
}

// Detach ends the attached session and returns ycode to driving itself.
func (a *App) Detach() (string, error) {
	att := a.attached
	if att == nil {
		return "", fmt.Errorf("not attached to an agent")
	}
	att.mu.Lock()
	defer att.mu.Unlock()

	_ = att.live.Quit()
	att.live.Close()
	// Whatever the agent said on its way out is the last thing it will ever say
	// here; render it rather than dropping it with the session.
	att.stream.Flush()
	elapsed := time.Since(att.started).Round(time.Second)
	a.attached = nil

	// PROVENANCE, in order of fidelity.
	//
	// The handoff note is the agent's own account and is authoritative. The
	// per-turn scrapes recorded during the session are the FALLBACK: a pty
	// merges stdout and stderr, so a scrape is a reconstruction that loses
	// whatever the agent repainted over.
	//
	// Whichever we end up with, the next agent is told WHICH — a scrape read as
	// though it were a verbatim record invites confident conclusions drawn from
	// text that was never quite what was said.
	summary := ""
	if rec := newHandoffSince(att.seenHandoffs); rec != nil {
		a.recordHandoffNote(att.target, elapsed, att.turns, summarizeHandoff(rec))
		summary = fmt.Sprintf("\n  It handed off via bashy (%s); that is the record of record.", rec.ID)
	} else {
		a.recordScrapeFallback(att.target, elapsed, att.turns)
		summary = "\n  It left no handoff note — the record above is a terminal scrape."
	}

	return fmt.Sprintf("← detached from %s — %d turn(s), %s%s",
		att.target, att.turns, elapsed, summary), nil
}

// recordScrapeFallback tells the next agent that the preceding exchange is a
// reconstruction rather than a verbatim record.
//
// Saying nothing would leave a scrape indistinguishable from an authored
// account, and a reader cannot discount what it does not know is uncertain.
func (a *App) recordScrapeFallback(target string, elapsed time.Duration, turns int) {
	if a.session == nil || turns == 0 {
		return
	}
	_ = a.session.AddMessage(session.ConversationMessage{
		Role:  session.RoleAssistant,
		Model: "ycode:handover",
		Content: []session.ContentBlock{{
			Type: session.ContentTypeText,
			Text: fmt.Sprintf(
				"[handover] %s ran for %s over %d turn(s) and left NO handoff note. The %s "+
					"exchange(s) above were captured by scraping its terminal, which is a "+
					"reconstruction: a pty merges stdout and stderr, so banners, spinners and "+
					"repainted text can survive while some of the real answer is lost. Treat it "+
					"as indicative rather than verbatim, and ask the user if a detail matters.",
				target, elapsed, turns, plural(turns)),
		}},
	})
}

func plural(n int) string {
	if n == 1 {
		return "single"
	}
	return "preceding"
}

// AttachedLabel names the currently attached agent, or "" when ycode is
// driving itself. The TUI uses it to show whose replies these are.
func (a *App) AttachedLabel() string {
	if a.attached == nil {
		return ""
	}
	return a.attached.target
}

// AttachedBinding is the tool:model actually answering right now, or "" when
// ycode answers for itself. The status bar needs the BINDING rather than the
// friendly label: "codex:gpt-5.5" says which model is producing the text,
// where a nickname alone does not.
func (a *App) AttachedBinding() string {
	if a.attached == nil {
		return ""
	}
	return a.attached.bindingLabel()
}

// screenControl matches the escape sequences that command the WHOLE TERMINAL
// rather than styling a span of text.
//
// Colour and style are safe to render inline and are deliberately KEPT: the
// other agent's output should look the way it looks. What cannot survive is
// anything that moves the cursor absolutely, erases the display, changes the
// scroll region or switches to the alternate screen — ycode owns the screen,
// and those would scribble outside its layout and corrupt both renderings.
//
// This is the difference between attach and --takeover. Under takeover the
// child really does own the terminal, so nothing needs filtering.
// "Final byte 'm' means colour" is NOT sufficient, and assuming it leaks
// terminal control into the transcript. `\x1b[>4m` (modifyOtherKeys) ends in
// 'm' but is a private-mode command, and it is exactly the sequence chat's
// sanitizer records as having leaked into the tail of every claude turn.
//
// The reliable discriminator is the PARAMETER, not the final byte: real SGR
// carries only digits and ';'. A CSI bearing a private marker (< > = ?) is
// never styling, whatever it ends with.
var screenControl = regexp.MustCompile(
	"\x1b\\[[<>=?][0-9;<>=?]*[ -/]*[@-~]" + // private-mode CSI — never SGR
		"|\x1b\\[[0-9;]*[ -/]*[@-ln-~]" + // ordinary CSI except final 'm'
		"|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)" + // OSC (window title, hyperlinks)
		"|\x1b[()][0-9A-Za-z]" + // charset select
		"|\x1b[0-9A-Za-z><=]") // ESC 7/8 cursor save-restore, ESC =

// ptyLineEnding matches the carriage returns a pty puts BEFORE a newline.
//
// It has to be normalised away first, and getting this wrong is what made attach
// render nothing at all for its entire existence. A pty in cooked mode ends every
// line with CR LF, and the agent CLIs observed here emit `\r\r\n` — so
// carriageRepaint below, which deletes everything from the start of a line up to
// a carriage return, was matching EVERY LINE and deleting all of it. A live
// session against claude rendered 42 bytes, all of them newlines. Not a subtle
// loss of fidelity: the whole feature showed a blank screen, and the operator had
// no way to know whether that meant "broken" or "still thinking".
var ptyLineEnding = regexp.MustCompile(`\r+\n`)

// columnJump is CHA — "move to column N". It is stripped as screen control
// everywhere else, and it must NOT be simply deleted here, because Claude Code
// uses it INSTEAD OF SPACES to lay out a line: `Quick\x1b[8Gsafety\x1b[15Gcheck:`
// is how it writes "Quick safety check:". Delete the sequences and the words are
// welded together.
//
// One space per jump, not a pad to the real column: the source was laid out for
// the pty's width and is being re-rendered into a viewport of a different one, so
// reproducing absolute columns would misalign anyway. A space restores the word
// boundary, which is the part that carries meaning.
var columnJump = regexp.MustCompile(`\x1b\[[0-9]*G`)

// carriageRepaint collapses a spinner's overwrite-in-place frames. A TUI draws
// them with \r + redraw, so the captured bytes hold every frame; only the last
// was ever on screen. Applied AFTER ptyLineEnding, so it only ever sees a
// carriage return that really is a repaint.
var carriageRepaint = regexp.MustCompile(`[^\n]*\r`)

// renderAgentOutput prepares another agent's captured output for display
// INSIDE ycode's viewport.
//
// It is deliberately NOT chat.SanitizeTurn: that strips every escape including
// SGR, which is right for text you intend to store or replay as data, and
// wrong for text you intend to show a human — it arrives colourless.
func renderAgentOutput(s string) string {
	return strings.TrimRight(sanitizeAgentChunk(s), " \t\n")
}

// sanitizeAgentChunk is renderAgentOutput without the trailing trim, so it is
// safe on a PIECE of a turn as well as a whole one.
//
// The trim is the one step that cannot be applied incrementally: mid-stream, the
// newline at the end of a chunk is the line break before the next chunk, not
// trailing whitespace, and eating it runs the agent's output together into one
// paragraph. Whole-turn callers still want it — a turn should not end in blank
// lines — so it stays, one level up.
// The ORDER of the steps below is load-bearing:
//
//	line endings → column jumps → screen control → carriage repaints
//
// Line endings first, so carriageRepaint cannot mistake a CR LF for a repaint and
// eat the line. Column jumps before screen control, because CHA is a CSI ending
// in 'G' and screenControl would otherwise swallow it before it can become the
// space it stands for.
func sanitizeAgentChunk(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = ptyLineEnding.ReplaceAllString(s, "\n")
	s = columnJump.ReplaceAllString(s, " ")
	s = screenControl.ReplaceAllString(s, "")
	s = carriageRepaint.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == 0x1b:
			return r // keep ESC: the SGR sequences we deliberately preserved
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			return -1
		default:
			return r
		}
	}, s)
}
