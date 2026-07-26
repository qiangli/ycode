package cli

import (
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
// considered finished. Same shape as foreman's steer loop.
const attachQuietPeriod = 2 * time.Second

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

	opt := chat.SessionOptions{
		Cwd:      a.workDir,
		ReadOnly: a.InPlanMode(),
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

	live, err := chat.Start(ctx, agentName, opt)
	if err != nil {
		return "", fmt.Errorf("attach to %s: %w", target.Label(), err)
	}

	a.attached = &attachedSession{
		live:         live,
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

	raw := att.live.Turn()
	out := strings.TrimSpace(renderAgentOutput(raw))
	if out == "" {
		// Silence is not an answer. Say so rather than render an empty turn
		// that looks like the agent replied with nothing.
		return "", fmt.Errorf("%s produced no output this turn", att.target)
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

// carriageRepaint collapses a spinner's overwrite-in-place frames. A TUI draws
// them with \r + redraw, so the captured bytes hold every frame; only the last
// was ever on screen.
var carriageRepaint = regexp.MustCompile(`[^\n]*\r`)

// renderAgentOutput prepares another agent's captured output for display
// INSIDE ycode's viewport.
//
// It is deliberately NOT chat.SanitizeTurn: that strips every escape including
// SGR, which is right for text you intend to store or replay as data, and
// wrong for text you intend to show a human — it arrives colourless.
func renderAgentOutput(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = screenControl.ReplaceAllString(s, "")
	s = carriageRepaint.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || r == 0x1b:
			return r // keep ESC: the SGR sequences we deliberately preserved
		case r < 0x20 || (r >= 0x7f && r < 0xa0):
			return -1
		default:
			return r
		}
	}, s)
	return strings.TrimRight(s, " \t\n")
}
