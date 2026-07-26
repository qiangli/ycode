package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/qiangli/ycode/internal/agentswitch"
	"github.com/qiangli/ycode/internal/commands"
	"github.com/qiangli/ycode/internal/runtime/session"
)

// Switch routes a /agent or /tool request to the mode it asked for.
//
// ATTACH IS THE DEFAULT. You stay in ycode, its replies render here, and the
// conversation keeps accumulating in one transcript — which is what makes
// switching again later carry everything. --takeover is the escape hatch for
// when you actually want the other tool's own full-screen UI.
func (a *App) Switch(ctx context.Context, req commands.SwitchRequest) (string, error) {
	if req.Takeover {
		return a.SwitchAgent(ctx, req)
	}
	return a.Attach(ctx, req)
}

// SwitchAgent hands this terminal to another fleet agent and returns when it
// exits, carrying the current conversation across by default.
//
// The carried context is the whole justification for the command. Running
// `bashy chat --agent codex` from a terminal is one line; doing it from inside
// ycode is only worth the plumbing because the work in progress comes with you.
func (a *App) SwitchAgent(ctx context.Context, req commands.SwitchRequest) (string, error) {
	if err := agentswitch.Guard(); err != nil {
		return "", err
	}

	// Resolve BEFORE checking the terminal. Both checks fail the call, but
	// they say different things, and the wrong order says the wrong one: a
	// typo would be reported as "needs a terminal" and helpfully suggest
	// running `bashy chat --agent codx -i`, a command that cannot work either.
	// Resolution is pure and cheap, so there is no cost to doing it first.
	target, err := agentswitch.Resolve(agentswitch.Request{Agent: req.Agent, Tool: req.Tool})
	if err != nil {
		return "", err
	}

	// An interactive handover needs a real terminal on BOTH sides: the TUI to
	// suspend, and a tty for the child to draw on. In --print or shell mode
	// neither exists, so refuse with the direct command rather than launching
	// a session nobody can see or type at.
	if a.ttyExec == nil || !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("/%s needs an interactive terminal; from here run:\n      bashy chat --%s %s -i",
			req.Verb, req.Verb, req.SelectorOrDefault())
	}

	sw := agentswitch.Request{
		Agent:       req.Agent,
		Tool:        req.Tool,
		Interactive: true,
		ReadOnly:    a.InPlanMode(),
		Cwd:         a.workDir,
		Mode:        agentswitch.ModeCarry,
	}
	if req.Fresh {
		sw.Mode = agentswitch.ModeFresh
	} else {
		sw.Context = a.handoffContext()
	}

	// ASK THE AGENT TO LEAVE A RECORD.
	//
	// ycode cannot capture a takeover — the child owns the terminal. But it can
	// ask the child to write its own account before it exits, and then read
	// that. The exchange stops being unrecoverable and becomes a handoff note
	// authored by the one participant who was actually there.
	//
	// It is an INSTRUCTION, not a mechanism: the agent may ignore it. So the
	// file's presence is the evidence. Present, we record what it says and
	// attribute it; absent, we record the gap instead. What we must never do is
	// assume compliance and leave the transcript looking continuous.
	handoffPath := a.handoffNotePath()
	sw.Context = appendHandoffInstruction(sw.Context, handoffPath)

	argv, _, err := agentswitch.Command(sw)
	if err != nil {
		return "", err
	}

	started := time.Now()
	res, err := a.ttyExec.ExecuteTTYRaw(ctx, shellJoin(argv), a.workDir, agentswitch.ChildEnv())
	if err != nil {
		return "", fmt.Errorf("switch to %s failed: %w", target.Label(), err)
	}

	elapsed := time.Since(started).Round(time.Second)

	// RECORD THE GAP.
	//
	// Under takeover the child owned the terminal and ycode captured nothing —
	// the script(1) wrapper that would have captured it is exactly what mangles
	// a nested TUI's rendering, so the two cannot both be had here.
	//
	// What must NOT happen is for the next agent to be handed a transcript that
	// runs straight from before the handover to after it, as though nothing
	// occurred. That reads as a continuous conversation and is a lie by
	// omission. A marker keeps the absence VISIBLE: the next agent is told
	// plainly that work happened which it cannot see, which is recoverable —
	// it can ask. Silence is not.
	note := a.readHandoffNote(handoffPath)
	if note != "" {
		a.recordHandoffNote(target.Label(), elapsed, res.ExitCode, note)
	} else {
		a.recordTakeoverGap(target.Label(), elapsed, res.ExitCode)
	}

	carried := "left a handoff note, folded into this conversation"
	if note == "" {
		carried = "left no handoff note — that exchange is NOT in this transcript"
	}
	return fmt.Sprintf("← back from %s — exit %d, %s\n  %s",
		target.Label(), res.ExitCode, elapsed, carried), nil
}

// handoffNotePath is where the agent is asked to write its account. Per
// session and per switch, so two handovers cannot overwrite each other.
func (a *App) handoffNotePath() string {
	dir := filepath.Join(os.TempDir(), "ycode-handoff")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, fmt.Sprintf("%s-%d.md", a.SessionID(), time.Now().UnixNano()))
}

// appendHandoffInstruction adds the request to the context the agent receives.
func appendHandoffInstruction(ctx, path string) string {
	instruction := fmt.Sprintf(
		"\n\n---\nBEFORE YOU EXIT: write a short account of this session to %s — "+
			"what you did, what you changed, what you concluded, and anything the next "+
			"agent needs to know. Plain markdown. The human is returning to another tool "+
			"that cannot see this terminal, and that file is the only way your work "+
			"reaches it.", path)
	return strings.TrimSpace(ctx) + instruction
}

// readHandoffNote returns the agent's account, or "" when it wrote none.
func (a *App) readHandoffNote(path string) string {
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// recordHandoffNote folds the agent's own account into the transcript.
//
// Attributed and labelled as a CLAIM: it is the agent's self-report, not
// something ycode observed. The next agent should weigh it accordingly.
func (a *App) recordHandoffNote(target string, elapsed time.Duration, exitCode int, note string) {
	if a.session == nil {
		return
	}
	_ = a.session.AddMessage(session.ConversationMessage{
		Role:  session.RoleAssistant,
		Model: "ycode:handover",
		Content: []session.ContentBlock{{
			Type: session.ContentTypeText,
			Text: fmt.Sprintf(
				"[handover] The user worked directly with %s for %s (exit %d). That exchange "+
					"was not captured, but the agent left this account of it — its own report, "+
					"not an observed transcript:\n\n%s",
				target, elapsed, exitCode, note),
		}},
	})
}

// recordTakeoverGap notes in ycode's transcript that a handover happened and
// that its content is unavailable.
func (a *App) recordTakeoverGap(target string, elapsed time.Duration, exitCode int) {
	if a.session == nil {
		return
	}
	_ = a.session.AddMessage(session.ConversationMessage{
		Role:  session.RoleAssistant,
		Model: "ycode:handover",
		Content: []session.ContentBlock{{
			Type: session.ContentTypeText,
			Text: fmt.Sprintf(
				"[handover] The user worked directly with %s for %s (exit %d). "+
					"That exchange happened in the agent's own terminal and was NOT captured, "+
					"so it is absent from this transcript. Ask the user what came of it rather "+
					"than assuming the conversation ran continuously.",
				target, elapsed, exitCode),
		}},
	})
}

// handoffContext renders the conversation for the target agent.
//
// This is a DETERMINISTIC export, not a summary: the slash path is meant to be
// free and instant, and asking the model to summarize first would cost a turn
// and a round-trip before the user got their agent. The tool-call path is where
// a model-written brief belongs, because there the model is already thinking.
func (a *App) handoffContext() string {
	if a.session == nil || len(a.session.Messages) == 0 {
		return ""
	}
	return commands.RenderHandoff(a.session, a.workDir)
}

// shellJoin renders argv for `sh -c`, quoting every element. The context we
// carry is multi-line prose containing quotes and newlines; naive
// concatenation would break the command apart.
func shellJoin(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`&|;<>()*?[]{}!#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// localOnlySlash names the commands that must run on the CLIENT even in
// thin-client mode, because they act on this terminal rather than on the
// conversation. Sending one to the server would start an interactive agent
// on the far end with no one attached to it.
var localOnlySlash = map[string]bool{
	"agent":  true,
	"tool":   true,
	"detach": true,
}

func isLocalOnlySlash(text string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(text, "/"), " ")
	return localOnlySlash[strings.ToLower(strings.TrimSpace(name))]
}
