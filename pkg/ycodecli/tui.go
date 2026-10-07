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
	host := &tuiHost{app: app, view: view, inv: inv, config: config, principal: principal}
	if queueRef, ok := steeringQueue(app, inv.Dispatch.AgentRef, inv.FrontendRef); ok {
		host.queueRef = queueRef
	}
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
	principal string
	queueRef  string
}

func (h *tuiHost) Turn(ctx context.Context, session, text string) (<-chan event.Event, error) {
	body, err := json.Marshal(map[string]string{h.inv.Dispatch.Input.PayloadKey: text})
	if err != nil {
		return nil, err
	}
	return h.view.Stream(ctx, body, frontend.Defaults{SessionID: session, Principal: h.principal})
}

func (h *tuiHost) Steer(session, text string) error {
	if h.queueRef == "" {
		return errors.New("the agent has no compiled steering queue")
	}
	return h.app.harness.Enqueue(public.QueueRequest{SessionID: session, QueueRef: h.queueRef, Class: "steering", Text: text})
}

func (h *tuiHost) TakeQueued(session string) ([]string, error) {
	if h.queueRef == "" {
		return nil, nil
	}
	return ownedTurnHost{app: h.app, session: session, queueRef: h.queueRef}.TakeQueued()
}

func (h *tuiHost) Settle(ctx context.Context, session string) bool {
	return h.app.harness.Settle(ctx, session)
}

func (h *tuiHost) Decide(ctx context.Context, session string, w tui.Waiting, action string) (<-chan event.Event, error) {
	return h.app.harness.Resume(ctx, public.ResumeRequest{
		SessionID: session, RunID: w.RunID, DecisionID: w.DecisionID, ExpectedVersion: w.Version,
		ReviewDigest: w.ReviewDigest, ReportDigest: w.ReportDigest, Action: action, Actor: h.principal,
	})
}

func (h *tuiHost) Payload(ref string) ([]byte, error) { return h.app.harness.Payload(ref) }

func (h *tuiHost) Status(session string) tui.Status {
	var status tui.Status
	status.Model, _ = defaultHarnessModel(h.app.doc)
	messages, err := h.app.harness.Transcript(session)
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
	entries, err := h.app.Transcript(session)
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
	switch s.Name {
	case "/save":
		found, err := h.app.harness.Session(session)
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
		var found public.SessionSummary
		var err error
		if len(args) > 0 {
			found, err = h.app.harness.Session(args[0])
		} else {
			var sessions []public.SessionSummary
			if sessions, err = h.app.harness.Sessions(); err == nil && len(sessions) == 0 {
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
		if len(args) > 0 {
			return tui.SlashResult{}, errors.New("switching the model for this session needs an engine route override that is not built yet; the compiled YAML is never changed")
		}
		current, err := h.declared(ctx, []string{"model", "current"})
		if err != nil {
			return tui.SlashResult{Output: current}, err
		}
		list, err := h.declared(ctx, []string{"model", "list"})
		return tui.SlashResult{Output: "current: " + current + list}, err
	case "/plan":
		out, err := h.declared(ctx, append([]string{"plan"}, args...))
		return tui.SlashResult{Output: out}, err
	case "/init":
		out, err := h.declared(ctx, append([]string{"init"}, args...))
		return tui.SlashResult{Output: out}, err
	}
	return tui.SlashResult{}, fmt.Errorf("%s is not a slash", s.Name)
}

// declared runs path (a subcommand path plus its arguments) through the
// compiled CLI in-process, after checking that the agent YAML declares it.
func (h *tuiHost) declared(ctx context.Context, argv []string) (string, error) {
	if !declaresCommand(h.app.doc.Spec.Interfaces.CLI.Root, argv) {
		return "", fmt.Errorf("the agent YAML declares no `%s` subcommand", argv[0])
	}
	root, err := harnesscli.New(h.app.doc, dispatchCLI, harnesscli.Options{Version: version, Commit: commit, IsTerminal: false, LookupEnv: os.LookupEnv})
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--file", h.config}, argv...))
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
