package ycodecli

// Sprint: #387; Story: #1625; Story-ID: 27b3dfdf7e76
//
// The `tui` frontend on a terminal: internal/harness/frontend/tui draws it,
// this file projects it onto the harness application. Turns go through the
// compiled tui frontend and its trigger, mid-turn lines through the agent's
// compiled steering queue, approvals through the typed Harness.Resume, and
// every slash through the declared subcommand it stands for.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/qiangli/yoke/pkg/llmbudget"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/frontend"
	"github.com/qiangli/ycode/internal/harness/frontend/tui"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// LiteralShell, when set, runs a literal command line typed in the TUI (the
// ladder's rung 0/1); bashy installs itself here so a line runs exactly as
// at its own prompt. Nil runs the line on the in-process shell interpreter.
var LiteralShell func(line string) *exec.Cmd

// LiteralCommands, when set, names the commands LiteralShell serves without
// PATH (bashy's in-process userland), so the ladder treats them as literal.
var LiteralCommands func() []string

func runTerminalTUI(ctx context.Context, app *harnessApplication, inv harnesscli.Invocation, session, principal string) error {
	view, err := frontend.NewTUI(app.doc, inv.FrontendRef, cliController{app, inv.Dispatch.AgentRef})
	if err != nil {
		return err
	}
	config, err := absSource(app.doc.Source)
	if err != nil {
		return err
	}
	host := &tuiHost{app: app, view: view, inv: inv, config: config, origin: inv.ConfigOrigin, principal: principal}
	if queueRef, ok := steeringQueue(app, inv.Dispatch.AgentRef, inv.FrontendRef); ok {
		host.queueRef = queueRef
	}
	defer func() {
		// The caller closes the application it opened; /config or /init
		// may have replaced it with one this terminal owns.
		if current := host.bound().app; current != app {
			_ = current.Close()
		}
	}()
	return tui.Run(ctx, tui.Options{Agent: app.doc.Metadata.Name, Session: session, Host: host, Shell: literalShell{}})
}

// steeringQueue is the agent's compiled queue when it has a steering class.
func steeringQueue(app *harnessApplication, agentRef, frontendRef string) (string, bool) {
	queueRef := app.queueFor(agentRef, frontendRef)
	if queueRef == "" {
		return "", false
	}
	_, ok := app.doc.Spec.Queues[queueRef].Priorities["steering"]
	return queueRef, ok
}

type tuiHost struct {
	app       *harnessApplication
	view      *frontend.TUI
	inv       harnesscli.Invocation
	config    string
	origin    string // how config was selected, as /config reports it
	principal string
	queueRef  string
	// mu guards the binding above: /config and /init replace it between
	// turns while status and transcript reads run concurrently.
	mu sync.Mutex
}

// tuiBinding is one consistent view of the configuration a host serves.
type tuiBinding struct {
	app            *harnessApplication
	view           *frontend.TUI
	inv            harnesscli.Invocation
	config, origin string
	queueRef       string
}

func (h *tuiHost) bound() tuiBinding {
	h.mu.Lock()
	defer h.mu.Unlock()
	return tuiBinding{app: h.app, view: h.view, inv: h.inv, config: h.config, origin: h.origin, queueRef: h.queueRef}
}

func (h *tuiHost) Turn(ctx context.Context, session, text string) (<-chan event.Event, error) {
	b := h.bound()
	body, err := json.Marshal(map[string]string{b.inv.Dispatch.Input.PayloadKey: text})
	if err != nil {
		return nil, err
	}
	return b.view.Stream(ctx, body, frontend.Defaults{SessionID: session, Principal: h.principal})
}

func (h *tuiHost) Steer(session, text string) error {
	b := h.bound()
	if b.queueRef == "" {
		return errors.New("the agent has no compiled steering queue")
	}
	return b.app.harness.Enqueue(public.QueueRequest{SessionID: session, QueueRef: b.queueRef, Class: "steering", Text: text})
}

func (h *tuiHost) TakeQueued(session string) ([]string, error) {
	b := h.bound()
	if b.queueRef == "" {
		return nil, nil
	}
	return ownedTurnHost{app: b.app, session: session, queueRef: b.queueRef}.TakeQueued()
}

func (h *tuiHost) Settle(ctx context.Context, session string) bool {
	b := h.bound()
	return b.app.harness.Settle(ctx, session)
}

func (h *tuiHost) Decide(ctx context.Context, session string, w tui.Waiting, action string) (<-chan event.Event, error) {
	b := h.bound()
	return b.app.harness.Resume(ctx, public.ResumeRequest{
		SessionID: session, RunID: w.RunID, DecisionID: w.DecisionID, ExpectedVersion: w.Version,
		ReviewDigest: w.ReviewDigest, ReportDigest: w.ReportDigest, Action: action, Actor: h.principal,
	})
}

func (h *tuiHost) Payload(ref string) ([]byte, error) {
	b := h.bound()
	return b.app.harness.Payload(ref)
}

func (h *tuiHost) Status(session string) tui.Status {
	b := h.bound()
	status := tui.Status{Agent: b.app.doc.Metadata.Name}
	// Model is what the provider last measured answering; Selected is the
	// session's explicit override, if any. Neither masks the other, so a
	// route fallback shows as the model that actually ran.
	status.Model, _ = defaultHarnessModel(b.app.doc)
	if ref, err := b.app.harness.SessionModelOverride(session); err == nil && ref != "" {
		if m, ok := b.app.doc.Spec.Models[ref]; ok {
			status.Selected = m.ID
		}
	}
	status.Mode, _ = b.app.harness.SessionMode(session)
	messages, err := b.app.harness.Transcript(session)
	if err != nil {
		return status
	}
	// Cost comes only from the llmbudget catalog: a session with any turn on
	// an unpriced model shows its tokens and no cost, never a guessed price.
	priced, latest := true, true
	for i := len(messages) - 1; i >= 0; i-- {
		usage := messages[i].Usage
		if usage == nil {
			continue
		}
		if latest {
			status.ContextTokens = usage.InputTokens + usage.CacheReadInput + usage.CacheCreationInput + usage.OutputTokens
			if messages[i].Model != "" {
				status.Model = messages[i].Model
			}
			latest = false
		}
		status.SessionTokens += usage.InputTokens + usage.OutputTokens
		cost, known := llmbudget.EstimatedCostUSD(messages[i].Model, int64(usage.InputTokens+usage.OutputTokens))
		priced = priced && known && messages[i].Model != ""
		status.CostUSD += cost
	}
	status.CostKnown = priced && status.SessionTokens > 0
	if !status.CostKnown {
		status.CostUSD = 0
	}
	return status
}

func (h *tuiHost) Transcript(session string) ([]tui.Entry, error) {
	b := h.bound()
	entries, err := b.app.Transcript(session)
	if err != nil {
		return nil, err
	}
	out := make([]tui.Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, tui.Entry{Role: e.Role, Text: e.Text})
	}
	return out, nil
}

// Sessions and Transcript make the application a frontend.SessionBrowser:
// the web chat's resume selector reads the very sessions the TUI resumes.
func (a *harnessApplication) Sessions() ([]frontend.SessionInfo, error) {
	sessions, err := a.harness.Sessions()
	if err != nil {
		return nil, err
	}
	out := make([]frontend.SessionInfo, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		out = append(out, frontend.SessionInfo{ID: sessions[i].ID, Title: sessions[i].Title, Updated: sessions[i].Updated})
	}
	return out, nil
}

func (a *harnessApplication) Transcript(session string) ([]frontend.TranscriptEntry, error) {
	messages, err := a.harness.Transcript(session)
	if err != nil {
		return nil, err
	}
	var entries []frontend.TranscriptEntry
	for _, m := range messages {
		var text []string
		for _, block := range m.Content {
			if block.Text != "" {
				text = append(text, block.Text)
			}
		}
		if len(text) > 0 {
			entries = append(entries, frontend.TranscriptEntry{Role: string(m.Role), Text: strings.Join(text, "\n")})
		}
	}
	return entries, nil
}

// Slash runs the declared subcommand a slash stands for. A slash whose
// subcommand the agent YAML does not declare fails; nothing is emulated.
func (h *tuiHost) Slash(ctx context.Context, session string, s tui.Slash, args []string) (tui.SlashResult, error) {
	b := h.bound()
	switch s.Name {
	case "/save":
		found, err := b.app.harness.Session(session)
		if err != nil {
			return tui.SlashResult{}, fmt.Errorf("nothing to save yet: session %s has no committed turn", session)
		}
		var out strings.Builder
		if len(args) > 0 {
			text, err := h.declared(ctx, append([]string{"session", "rename", found.ID}, args...))
			out.WriteString(text)
			if err != nil {
				return tui.SlashResult{Output: out.String()}, err
			}
		}
		fmt.Fprintf(&out, "saved: session %s is durable; /resume %s here or `resume %s` from a terminal continues it", found.ID, found.ID, found.ID)
		return tui.SlashResult{Output: out.String()}, nil
	case "/resume":
		// A live cooperative pause on this session is released by Continue;
		// a typed HITL approval is never answered here, it stays y/n.
		continued, declared := declaredCommand(b.app.doc.Spec.Interfaces.CLI.Root, []string{"continue"})
		if declared && continued.Dispatch.Operation == "session" && continued.Dispatch.Action == "continue" && (len(args) == 0 || args[0] == session) {
			if flag, ok := requiredFlag(b.app.doc.Spec.Interfaces.CLI.Root, []string{"continue"}); ok {
				return tui.SlashResult{}, fmt.Errorf("`continue` requires --%s, which a slash cannot supply; run the command from a terminal", flag)
			}
			_, err := sessionControl(ctx, b.app, *continued.Dispatch, session, h.principal, nil)
			switch {
			case err == nil:
				return tui.SlashResult{Output: "continued: the paused turn resumes at its next step (a pending approval still waits for y/n)"}, nil
			case !errors.Is(err, public.ErrNoLiveTurn) && !errors.Is(err, public.ErrNoPausedRun):
				// A real control or persistence failure is never hidden
				// behind session navigation.
				return tui.SlashResult{}, err
			}
		}
		var found public.SessionSummary
		var err error
		if len(args) > 0 {
			found, err = b.app.harness.Session(args[0])
		} else {
			var sessions []public.SessionSummary
			if sessions, err = b.app.harness.Sessions(); err == nil && len(sessions) == 0 {
				err = errors.New("no session to resume")
			} else if err == nil {
				found = sessions[0]
			}
		}
		if err != nil {
			return tui.SlashResult{}, err
		}
		if found.ID == session {
			return tui.SlashResult{Output: "already on session " + session}, nil
		}
		return tui.SlashResult{Session: found.ID}, nil
	case "/model":
		return h.model(ctx, session, args)
	case "/plan":
		return h.command(ctx, session, []string{"plan"}, args)
	case "/clear":
		// The declared `clear` starts the new session; the harness refuses
		// it while a turn or approval is live, so nothing is cancelled.
		command, ok := declaredCommand(b.app.doc.Spec.Interfaces.CLI.Root, []string{"clear"})
		if !ok || command.Dispatch == nil || command.Dispatch.Operation != "session" || command.Dispatch.Action != "clear" {
			return tui.SlashResult{}, errors.New("the agent YAML declares no `clear` session command")
		}
		if len(args) > 0 {
			return tui.SlashResult{}, errors.New("/clear takes no arguments")
		}
		result, err := sessionControl(ctx, b.app, *command.Dispatch, session, h.principal, nil)
		if err != nil {
			return tui.SlashResult{}, err
		}
		model := "the configured default"
		if result.ModelRef != "" {
			model = fmt.Sprintf("%s (%s)", result.ModelRef, b.app.doc.Spec.Models[result.ModelRef].ID)
		}
		out := fmt.Sprintf("cleared: new session %s on %s, model %s\nsession %s is kept; /resume %s returns to it", result.SessionID, b.config, model, session, session)
		return tui.SlashResult{Output: out, Session: result.SessionID, Fresh: true, Clear: true}, nil
	case "/config":
		if len(args) == 0 {
			out, err := h.declared(ctx, []string{"config", "source"})
			return tui.SlashResult{Output: out}, err
		}
		if len(args) > 1 {
			return tui.SlashResult{}, errors.New("/config takes one FILE")
		}
		if b.app.harness.Busy() {
			return tui.SlashResult{}, errors.New("a turn is running; /config FILE switches only between turns")
		}
		// The declared `config use` validates the candidate; a failure
		// keeps the effective configuration as it was.
		out, err := h.declared(ctx, []string{"config", "use", args[0]})
		if err != nil {
			return tui.SlashResult{Output: out}, err
		}
		path, err := filepath.Abs(args[0])
		if err != nil {
			return tui.SlashResult{Output: out}, err
		}
		return h.rebind(b, session, path, "/config in this terminal", out)
	case "/init":
		if len(args) > 0 {
			return tui.SlashResult{}, errors.New("/init takes no arguments")
		}
		if b.app.harness.Busy() {
			return tui.SlashResult{}, errors.New("a turn is running; /init runs only between turns")
		}
		out, err := h.declared(ctx, []string{"init"})
		if err != nil {
			return tui.SlashResult{Output: out}, err
		}
		// Recompile so the instructions reach the next turn's context.
		return h.rebind(b, session, b.config, b.origin, out)
	}
	return tui.SlashResult{}, fmt.Errorf("%s is not a slash", s.Name)
}

// rebind serves the configuration at path from the next turn on. It
// compiles path afresh (an unchanged digest keeps everything as it is),
// requires the same terminal route, and keeps a session only when nothing
// in it is bound to the previous configuration: a session with committed
// turns, a model selection or a mode moves to a new session, and stays
// resumable with /resume.
func (h *tuiHost) rebind(b tuiBinding, session, path, origin, prior string) (tui.SlashResult, error) {
	var out strings.Builder
	out.WriteString(strings.TrimRight(prior, "\n"))
	if out.Len() > 0 {
		out.WriteString("\n")
	}
	app, err := openHarnessApplication(path, b.app.options...)
	if err != nil {
		return tui.SlashResult{Output: out.String()}, fmt.Errorf("configuration unchanged: %w", err)
	}
	if path == b.config && app.doc.ConfigDigest == b.app.doc.ConfigDigest {
		_ = app.Close()
		fmt.Fprintf(&out, "configuration unchanged: %s (%s)", path, b.app.doc.ConfigDigest)
		return tui.SlashResult{Output: out.String()}, nil
	}
	inv := b.inv
	inv.ConfigFile, inv.ConfigOrigin = path, origin
	if path != b.config {
		// Another file brings its own route: its root input's terminal
		// frontend, which must be a tui.
		root := app.doc.Spec.Interfaces.CLI.Root.Dispatch
		if root == nil || root.Operation != "input" || root.Input == nil || root.Input.TerminalFrontendRef == "" {
			_ = app.Close()
			return tui.SlashResult{Output: out.String()}, fmt.Errorf("configuration unchanged: %s declares no terminal route", path)
		}
		inv.Dispatch, inv.FrontendRef = *root, root.Input.TerminalFrontendRef
	}
	view, err := frontend.NewTUI(app.doc, inv.FrontendRef, cliController{app, inv.Dispatch.AgentRef})
	if err != nil {
		_ = app.Close()
		return tui.SlashResult{Output: out.String()}, fmt.Errorf("configuration unchanged: %w", err)
	}
	queueRef, _ := steeringQueue(app, inv.Dispatch.AgentRef, inv.FrontendRef)
	next := session
	_, committed := b.app.harness.Session(session)
	_, modelErr := app.harness.SessionModelOverride(session)
	_, modeErr := app.harness.SessionMode(session)
	if committed == nil || modelErr != nil || modeErr != nil {
		next = uuid.NewString()
	}
	h.mu.Lock()
	if h.app != b.app {
		h.mu.Unlock()
		_ = app.Close()
		return tui.SlashResult{Output: out.String()}, errors.New("configuration unchanged: it was replaced meanwhile")
	}
	previous := h.app
	h.app, h.view, h.inv, h.config, h.origin, h.queueRef = app, view, inv, path, origin, queueRef
	h.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
	fmt.Fprintf(&out, "configuration: %s (%s, %s) from the next turn", path, app.doc.Metadata.Name, app.doc.ConfigDigest)
	if next != session {
		fmt.Fprintf(&out, "\nnew session %s; session %s keeps its configuration and stays resumable with /resume %s", next, session, session)
		return tui.SlashResult{Output: out.String(), Session: next, Fresh: true}, nil
	}
	return tui.SlashResult{Output: out.String()}, nil
}

// model projects `model current` and `model list` (no NAME) or
// `model use NAME` as the agent YAML declares them.
func (h *tuiHost) model(ctx context.Context, session string, args []string) (tui.SlashResult, error) {
	if len(args) > 0 {
		return h.command(ctx, session, []string{"model", "use"}, args)
	}
	current, err := h.command(ctx, session, []string{"model", "current"}, nil)
	if err != nil {
		return current, err
	}
	list, err := h.command(ctx, session, []string{"model", "list"}, nil)
	list.Output = strings.TrimRight(current.Output, "\n") + "\n" + strings.TrimRight(list.Output, "\n") + "\n/model NAME selects one of the agent's declared route for this session"
	return list, err
}

// command runs the declared command at path as its authored dispatch says.
// A session action goes through sessionControl, the same resolution and
// admission the CLI command uses, on this terminal's session and principal;
// a turn control comes back as a turn bound to that session, which the
// terminal streams, steers and stops like any other. Any other operation
// runs through the compiled CLI.
func (h *tuiHost) command(ctx context.Context, session string, path, args []string) (tui.SlashResult, error) {
	b := h.bound()
	command, ok := declaredCommand(b.app.doc.Spec.Interfaces.CLI.Root, path)
	if !ok {
		return tui.SlashResult{}, fmt.Errorf("the agent YAML declares no `%s` subcommand", strings.Join(path, " "))
	}
	route := *command.Dispatch
	if route.Operation != "session" {
		out, err := h.declared(ctx, append(slices.Clone(path), args...))
		return tui.SlashResult{Output: out}, err
	}
	// The shortcut carries positional arguments only; a required authored
	// flag cannot be supplied through it, so it is refused, never bypassed.
	if flag, ok := requiredFlag(b.app.doc.Spec.Interfaces.CLI.Root, path); ok {
		return tui.SlashResult{}, fmt.Errorf("`%s` requires --%s, which a slash cannot supply; run the command from a terminal", strings.Join(path, " "), flag)
	}
	if len(args) < command.Args.Min || (command.Args.Max >= 0 && len(args) > command.Args.Max) {
		return tui.SlashResult{}, fmt.Errorf("`%s` takes %d to %d arguments as declared", strings.Join(path, " "), command.Args.Min, command.Args.Max)
	}
	turn := route.Action == "retry" || route.Action == "btw" || (route.Action == "plan" && len(args) > 0)
	if turn {
		text := strings.Join(args, " ")
		if text == "" {
			text = "(" + strings.Join(path, " ") + ")"
		}
		result := tui.SlashResult{Turn: text}
		if route.Action == "plan" {
			result.Output = "mode: plan — this turn plans without running tools; /plan again returns to act"
		}
		result.Start = func(ctx context.Context) (<-chan event.Event, error) {
			result, err := sessionControl(ctx, b.app, route, session, h.principal, args)
			return result.Stream, err
		}
		return result, nil
	}
	result, err := sessionControl(ctx, b.app, route, session, h.principal, args)
	if err != nil {
		return tui.SlashResult{}, err
	}
	if result.Stream != nil {
		for range result.Stream {
		}
	}
	switch route.Action {
	case "model-current":
		return tui.SlashResult{Output: fmt.Sprintf("current: %s (%s)", result.ModelRef, b.app.doc.Spec.Models[result.ModelRef].ID)}, nil
	case "model-use":
		return tui.SlashResult{Output: fmt.Sprintf("model: session %s now uses %s (%s)", session, result.ModelRef, b.app.doc.Spec.Models[result.ModelRef].ID)}, nil
	case "plan":
		mode, err := b.app.harness.SessionMode(session)
		if err != nil {
			return tui.SlashResult{}, err
		}
		if mode == "plan" {
			return tui.SlashResult{Output: "mode: plan — turns plan without running tools; /plan again returns to act"}, nil
		}
		return tui.SlashResult{Output: "mode: act — turns run tools again"}, nil
	}
	data, err := json.Marshal(result.Value)
	return tui.SlashResult{Output: string(data)}, err
}

// declared runs path (a subcommand path plus its arguments) through the
// compiled CLI in-process, after checking that the agent YAML declares it.
func (h *tuiHost) declared(ctx context.Context, argv []string) (string, error) {
	b := h.bound()
	if !declaresCommand(b.app.doc.Spec.Interfaces.CLI.Root, argv) {
		return "", fmt.Errorf("the agent YAML declares no `%s` subcommand", argv[0])
	}
	root, err := harnesscli.New(b.app.doc, dispatchCLI, harnesscli.Options{Version: version, Commit: commit, IsTerminal: false, LookupEnv: os.LookupEnv, ConfigOrigin: b.origin})
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--file", b.config}, argv...))
	err = root.ExecuteContext(ctx)
	return out.String(), err
}

// declaresCommand reports whether argv begins with a declared, dispatchable
// subcommand path.
func declaresCommand(root harnessspec.CLICommand, argv []string) bool {
	command := root
	matched := false
	for _, word := range argv {
		next, ok := childCommand(command, word)
		if !ok {
			break
		}
		command, matched = next, true
	}
	return matched && command.Dispatch != nil
}

// declaredCommand is the dispatchable command at exactly path.
func declaredCommand(root harnessspec.CLICommand, path []string) (harnessspec.CLICommand, bool) {
	command := root
	for _, word := range path {
		next, ok := childCommand(command, word)
		if !ok {
			return harnessspec.CLICommand{}, false
		}
		command = next
	}
	return command, len(path) > 0 && command.Dispatch != nil
}

// requiredFlag names a required flag the command at path declares or
// inherits from an ancestor.
func requiredFlag(root harnessspec.CLICommand, path []string) (string, bool) {
	command := root
	for i := 0; ; i++ {
		for _, flag := range command.Flags {
			if flag.Required && (i == len(path) || flag.Scope == "inherited") {
				return flag.Name, true
			}
		}
		if i == len(path) {
			return "", false
		}
		next, ok := childCommand(command, path[i])
		if !ok {
			return "", false
		}
		command = next
	}
}

func childCommand(parent harnessspec.CLICommand, name string) (harnessspec.CLICommand, bool) {
	for _, child := range parent.Commands {
		if child.Name == name {
			return child, true
		}
		for _, alias := range child.Aliases {
			if alias == name {
				return child, true
			}
		}
	}
	return harnessspec.CLICommand{}, false
}

// literalShell answers the action ladder from the process environment and
// runs a literal line on the user's terminal.
type literalShell struct{}

func (literalShell) Known(name string) bool {
	if interp.IsBuiltin(name) {
		return true
	}
	for _, keyword := range []string{"if", "for", "while", "until", "case", "function", "select", "time", "!", "[[", "{"} {
		if name == keyword {
			return true
		}
	}
	if strings.ContainsRune(name, '/') {
		info, err := os.Stat(name)
		return err == nil && !info.IsDir()
	}
	if LiteralCommands != nil && slices.Contains(LiteralCommands(), name) {
		return true
	}
	return onPathExact(name)
}

func (literalShell) Names() []string {
	names := interp.BuiltinNames()
	if LiteralCommands != nil {
		names = append(names, LiteralCommands()...)
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
				names = append(names, entry.Name())
			}
		}
	}
	return names
}

func (literalShell) GlobMatches(pattern string) bool {
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

func (literalShell) Command(line string) tui.ExecCommand {
	if LiteralShell != nil {
		return osCommand{LiteralShell(line)}
	}
	return &interpCommand{line: line, stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
}

// onPathExact finds name on PATH spelled exactly (a case-insensitive file
// system must not turn "Read the file" into /usr/bin/read).
func onPathExact(name string) bool {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = append(exts, ".exe", ".cmd", ".bat", ".com")
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			for _, ext := range exts {
				if entry.Name() == name+ext {
					if info, err := entry.Info(); err == nil && !info.IsDir() && (info.Mode()&0o111 != 0 || runtime.GOOS == "windows") {
						return true
					}
				}
			}
		}
	}
	return false
}

// osCommand hands a process the terminal.
type osCommand struct{ *exec.Cmd }

func (c osCommand) SetStdin(r io.Reader)  { c.Stdin = r }
func (c osCommand) SetStdout(w io.Writer) { c.Stdout = w }
func (c osCommand) SetStderr(w io.Writer) { c.Stderr = w }

// interpCommand runs one line on the in-process shell interpreter.
type interpCommand struct {
	line           string
	stdin          io.Reader
	stdout, stderr io.Writer
}

func (c *interpCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *interpCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *interpCommand) SetStderr(w io.Writer) { c.stderr = w }

func (c *interpCommand) Run() error {
	file, err := syntax.NewParser().Parse(strings.NewReader(c.line), "")
	if err != nil {
		return err
	}
	runner, err := interp.New(interp.StdIO(c.stdin, c.stdout, c.stderr))
	if err != nil {
		return err
	}
	err = runner.Run(context.Background(), file)
	var status interp.ExitStatus
	if errors.As(err, &status) {
		return fmt.Errorf("exit status %d", status)
	}
	return err
}
