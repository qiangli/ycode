// Package tui is the terminal control of the `tui` frontend kind: an input
// box with history and slash completion, the transcript printed into the
// terminal's own scrollback as events arrive, a status line, the in-turn
// approval prompt, mid-turn steering and ESC.
//
// It is presentation only. Turns, steering, approval, sessions and the slash
// subcommands all go through Host, which projects them onto the compiled
// harness; nothing here chooses a route, a policy or a model.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/qiangli/bashy/pkg/ladder"
	"github.com/qiangli/ycode/internal/harness/event"
)

// Waiting is an approval the running turn is blocked on, as carried by the
// canonical hitl.waiting event. Its identity and digests bind the decision.
type Waiting struct {
	RunID        string
	PolicyRef    string
	DecisionID   string
	Version      uint64
	ReviewDigest string
	ReportDigest string
}

// Status is what the status line shows besides the agent and session.
type Status struct {
	Agent         string // the configuration now served; "" keeps Options.Agent
	Model         string // the model last measured answering (or requested, mid-turn)
	Selected      string // the session's explicit model override; "" = route default
	Mode          string // the session's durable mode: "plan" shows on the line
	ContextTokens int
	SessionTokens int     // input + output across the session's turns
	CostUSD       float64 // meaningful only when CostKnown
	CostKnown     bool    // every turn's model is priced in the catalog
}

// Entry is one transcript message shown when a session is resumed.
type Entry struct{ Role, Text string }

// SlashResult is what a slash subcommand printed, the session the
// terminal moves to when the slash switched it ("" stays), and a turn the
// slash admits: Start opens it on the session the slash ran on, and the
// terminal shows Turn as its echo (nil Start: none).
type SlashResult struct {
	Output  string
	Session string
	Fresh   bool // Session is new: nothing to replay
	Clear   bool // a clean conversation: the screen's transcript is cleared too
	Rebound bool // the configuration changed under the same session
	Turn    string
	Start   func(ctx context.Context) (<-chan event.Event, error)
}

// Host projects the terminal onto the harness.
type Host interface {
	// Turn submits text as one turn on session and streams its events.
	Turn(ctx context.Context, session, text string) (<-chan event.Event, error)
	// Steer queues a line typed during a running turn on session.
	Steer(session, text string) error
	// TakeQueued returns the lines no turn drained.
	TakeQueued(session string) ([]string, error)
	// Settle waits for a cancelled turn to record its end.
	Settle(ctx context.Context, session string) bool
	// Decide resolves the approval the running turn waits on.
	Decide(ctx context.Context, session string, w Waiting, action string) (<-chan event.Event, error)
	// Payload returns a content-addressed payload body.
	Payload(ref string) ([]byte, error)
	// Slash runs the subcommand a slash stands for.
	Slash(ctx context.Context, session string, s Slash, args []string) (SlashResult, error)
	// Status reports the status line for session.
	Status(session string) Status
	// Transcript returns session's committed messages.
	Transcript(session string) ([]Entry, error)
}

// ExecCommand is a literal command line handed the terminal while it runs.
type ExecCommand interface {
	Run() error
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	SetStderr(io.Writer)
}

// Shell answers the action ladder and runs literal command lines (rung 0
// byte-identically, rung 1 repaired and echoed).
type Shell interface {
	ladder.Commands
	Command(line string) ExecCommand
}

// Options configures one terminal.
type Options struct {
	Agent   string
	Session string
	Host    Host
	Shell   Shell // nil: every non-slash line is a turn
	Input   io.Reader
	Output  io.Writer
}

// Run hosts the terminal until the user leaves it.
func Run(ctx context.Context, opts Options) error {
	if opts.Host == nil || opts.Session == "" {
		return errors.New("tui requires a host and a session")
	}
	m := newModel(ctx, opts)
	programOptions := []tea.ProgramOption{tea.WithContext(ctx)}
	if opts.Input != nil {
		programOptions = append(programOptions, tea.WithInput(opts.Input))
	}
	if opts.Output != nil {
		programOptions = append(programOptions, tea.WithOutput(opts.Output))
	}
	final, err := tea.NewProgram(m, programOptions...).Run()
	if fm, ok := final.(*model); ok && fm.cancel != nil {
		fm.cancel()
	}
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

var (
	faint  = lipgloss.NewStyle().Faint(true)
	strong = lipgloss.NewStyle().Bold(true)
	failed = lipgloss.NewStyle().Foreground(lipgloss.Red)
)

type model struct {
	ctx     context.Context
	opts    Options
	session string
	input   textinput.Model
	history []string
	recall  int
	width   int

	running     bool
	started     time.Time
	gen         int
	cancel      context.CancelFunc
	seen        map[uint64]bool
	activity    string
	waiting     *Waiting
	interrupted bool
	held        []string // lines the queue refused; run after the turn
	busy        string   // a slash in flight
	pending     []string // lines typed while a slash ran; submitted after it
	status      Status
	live        strings.Builder // streamed text of the current model call not yet printed
	streamed    string          // text already printed from llm.delta this turn
	// epoch fences asynchronous status reads: a session or configuration
	// change bumps it, and a read issued before the change is dropped.
	epoch int
}

func newModel(ctx context.Context, opts Options) *model {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "ask, run a command, or /help"
	in.Focus()
	return &model{ctx: ctx, opts: opts, session: opts.Session, input: in, width: 80, status: opts.Host.Status(opts.Session)}
}

type (
	eventMsg struct {
		gen    int
		stream <-chan event.Event
		ev     event.Event
		ok     bool
		main   bool
	}
	turnDoneMsg struct {
		gen    int
		queued []string
		err    error
	}
	slashDoneMsg struct {
		session string // the session the slash ran on
		result  SlashResult
		err     error
	}
	execDoneMsg   struct{ err error }
	decideDoneMsg struct {
		gen    int
		stream <-chan event.Event
		err    error
	}
	statusMsg struct {
		epoch  int
		status Status
	}
)

func (m *model) Init() tea.Cmd {
	banner := tea.Println(faint.Render(fmt.Sprintf("%s · session %s — ask in plain words, run a command, or /help. Ctrl-D leaves.", m.opts.Agent, m.session)))
	// A terminal opened on a session with history shows its tail first.
	if entries, err := m.opts.Host.Transcript(m.session); err == nil && len(entries) > 0 {
		return tea.Sequence(banner, m.replay())
	}
	return banner
}

// reset drops everything transient that belongs to the conversation on
// screen: streamed text, activity, a waiting approval and the turn
// generation, so a late event, turn end or status read from before a
// session or configuration change never lands in the next conversation.
func (m *model) reset() {
	m.gen++
	m.epoch++
	m.live.Reset()
	m.streamed, m.activity, m.waiting, m.interrupted = "", "", nil, false
	m.seen, m.held = map[uint64]bool{}, nil
}

func wait(gen int, stream <-chan event.Event, main bool) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-stream
		return eventMsg{gen: gen, stream: stream, ev: ev, ok: ok, main: main}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.SetWidth(max(10, msg.Width-4))
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case eventMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		if !msg.ok {
			if !msg.main {
				return m, nil
			}
			return m, m.endTurn()
		}
		cmds := []tea.Cmd{wait(msg.gen, msg.stream, msg.main)}
		if !m.seen[msg.ev.Sequence] {
			m.seen[msg.ev.Sequence] = true
			cmds = append(cmds, m.render(msg.ev))
		}
		return m, tea.Batch(cmds...)
	case decideDoneMsg:
		if msg.err != nil {
			return m, tea.Println(failed.Render("ycode: approval: " + msg.err.Error()))
		}
		if msg.gen != m.gen {
			return m, nil
		}
		return m, wait(msg.gen, msg.stream, false)
	case turnDoneMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.running, m.waiting, m.activity = false, nil, ""
		var out []tea.Cmd
		if msg.err != nil {
			out = append(out, tea.Println(failed.Render("ycode: "+msg.err.Error())))
		}
		next := append(msg.queued, m.held...)
		m.held = nil
		if len(next) > 0 {
			if m.interrupted {
				out = append(out, tea.Println(faint.Render("interrupted; running what was typed during it")))
			}
			m.interrupted = false
			out = append(out, m.startTurn(strings.Join(next, "\n")))
			return m, tea.Sequence(out...)
		}
		if m.interrupted {
			out = append(out, tea.Println(faint.Render("interrupted: the turn stopped before its next step; the session continues")))
			m.interrupted = false
		} else {
			out = append(out, tea.Println(faint.Render(fmt.Sprintf("· turn ended in %.1fs", time.Since(m.started).Seconds()))))
		}
		out = append(out, m.refreshStatus())
		return m, tea.Sequence(out...)
	case slashDoneMsg:
		m.busy = ""
		var out []tea.Cmd
		if text := strings.TrimRight(msg.result.Output, "\n"); text != "" {
			out = append(out, tea.Println(text))
		}
		if msg.err != nil {
			out = append(out, tea.Println(failed.Render("ycode: "+msg.err.Error())))
		}
		switch {
		case msg.session != m.session:
			// Lines wait while a slash runs, so this is not reached today;
			// a result never acts on a session it was not issued for.
			out = append(out, tea.Println(faint.Render("ignored: "+msg.result.Turn+" was issued on session "+msg.session)))
		case msg.result.Session != "" && msg.result.Session != m.session && m.running:
			out = append(out, tea.Println(faint.Render("finish or ESC this turn before switching sessions")))
		case msg.result.Session != "" && msg.result.Session != m.session:
			m.session = msg.result.Session
			m.reset()
			if msg.result.Clear {
				// The old transcript leaves the screen with its session;
				// the slash's own report is reprinted on the clean screen.
				out = []tea.Cmd{tea.ClearScreen}
				if text := strings.TrimRight(msg.result.Output, "\n"); text != "" {
					out = append(out, tea.Println(text))
				}
			}
			if !msg.result.Fresh {
				out = append(out, m.replay())
			}
		case msg.result.Rebound && !m.running:
			// Same session, another configuration: nothing streamed or
			// read under the previous one carries over.
			m.reset()
		case msg.result.Start != nil && m.running:
			out = append(out, tea.Println(failed.Render("ycode: a turn is running; "+msg.result.Turn+" was not started")))
		case msg.result.Start != nil:
			out = append(out, m.startWith(msg.result.Turn, msg.result.Start))
		}
		out = append(out, m.refreshStatus())
		out = append(out, m.drainPending()...)
		return m, tea.Sequence(out...)
	case execDoneMsg:
		if msg.err != nil {
			return m, tea.Println(faint.Render(msg.err.Error()))
		}
		return m, nil
	case statusMsg:
		if msg.epoch == m.epoch {
			m.status = msg.status
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.waiting != nil && m.input.Value() == "" {
		switch key {
		case "y", "Y":
			return m, m.decide("approve")
		case "n", "N":
			return m, m.decide("reject")
		}
	}
	switch key {
	case "ctrl+c":
		if m.running {
			return m, m.interrupt()
		}
		if m.input.Value() != "" {
			m.input.Reset()
			return m, nil
		}
		return m, tea.Quit
	case "ctrl+d":
		if !m.running && m.input.Value() == "" {
			return m, tea.Quit
		}
	case "esc":
		if m.running {
			return m, m.interrupt()
		}
		return m, nil
	case "tab":
		value := m.input.Value()
		if strings.HasPrefix(value, "/") && !strings.Contains(value, " ") {
			if names := completeSlash(value); len(names) == 1 {
				m.input.SetValue(names[0] + " ")
				m.input.CursorEnd()
			} else if p := commonPrefix(names); len(p) > len(value) {
				m.input.SetValue(p)
				m.input.CursorEnd()
			} else if len(names) > 1 {
				return m, tea.Println(faint.Render(strings.Join(names, "  ")))
			}
		}
		return m, nil
	case "up":
		if m.recall > 0 {
			m.recall--
			m.input.SetValue(m.history[m.recall])
			m.input.CursorEnd()
		}
		return m, nil
	case "down":
		if m.recall < len(m.history) {
			m.recall++
			value := ""
			if m.recall < len(m.history) {
				value = m.history[m.recall]
			}
			m.input.SetValue(value)
			m.input.CursorEnd()
		}
		return m, nil
	case "enter":
		line := m.input.Value()
		m.input.Reset()
		if strings.TrimSpace(line) == "" {
			return m, nil
		}
		if len(m.history) == 0 || m.history[len(m.history)-1] != line {
			m.history = append(m.history, line)
		}
		m.recall = len(m.history)
		return m, m.submit(line)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// submit routes one line: during a turn it steers; otherwise a slash runs
// its subcommand, a command line runs as typed, and free text is a turn.
func (m *model) submit(line string) tea.Cmd {
	if m.running {
		// /resume during a turn is never steering text: on this session it
		// releases a live pause; naming another session is refused, since
		// switching away mid-turn would orphan the running turn.
		if slash, args, ok, _ := parseSlash(line); ok && slash.Name == "/resume" {
			if len(args) == 0 || (len(args) == 1 && args[0] == m.session) {
				return m.slash(slash, args)
			}
			return tea.Println(failed.Render("ycode: /resume " + strings.Join(args, " ") + " refused during a turn; stop or finish this turn first"))
		}
		if slash, _, ok, _ := parseSlash(line); ok && slash.Idle {
			return tea.Println(failed.Render("ycode: " + slash.Name + " refused during a turn; stop or finish this turn first"))
		}
		if err := m.opts.Host.Steer(m.session, line); err != nil {
			m.held = append(m.held, line)
			return tea.Println(faint.Render("↳ held for after this turn: " + line + " (" + err.Error() + ")"))
		}
		return tea.Println(faint.Render("↳ steer: " + line))
	}
	if m.busy != "" {
		// A slash may switch sessions or admit a turn; what was typed waits
		// for it so it lands on the session and turn the slash leaves.
		m.pending = append(m.pending, line)
		return tea.Println(faint.Render("↳ held until " + m.busy + " finishes: " + line))
	}
	if slash, args, ok, err := parseSlash(line); ok {
		if err != nil {
			return tea.Println(failed.Render("ycode: " + slash.Name + ": " + err.Error() + "; quote a FILE with spaces as '...' or \"...\""))
		}
		return m.slash(slash, args)
	}
	if word, _, _ := strings.Cut(strings.TrimSpace(line), " "); strings.HasPrefix(word, "/") && !strings.Contains(word[1:], "/") {
		return tea.Println(failed.Render("ycode: unknown slash " + word + "; /help lists them"))
	}
	if m.opts.Shell != nil {
		d := ladder.Resolve(line, m.opts.Shell)
		switch d.Rung {
		case ladder.Literal, ladder.Repair:
			echo := "$ " + d.Line
			if d.Rung == ladder.Repair {
				echo += "  (" + d.Note + ")"
			}
			return tea.Sequence(tea.Println(strong.Render(echo)), tea.Exec(m.opts.Shell.Command(d.Line), func(err error) tea.Msg { return execDoneMsg{err} }))
		}
		line = d.Line
	}
	return m.startTurn(line)
}

// drainPending submits the lines held during a slash, in order, until one
// starts another slash.
func (m *model) drainPending() []tea.Cmd {
	var out []tea.Cmd
	for len(m.pending) > 0 && m.busy == "" {
		line := m.pending[0]
		m.pending = m.pending[1:]
		out = append(out, m.submit(line))
	}
	return out
}

func (m *model) startTurn(text string) tea.Cmd {
	session, host := m.session, m.opts.Host
	return m.startWith(text, func(ctx context.Context) (<-chan event.Event, error) { return host.Turn(ctx, session, text) })
}

// startWith runs start as this terminal's turn, echoing text.
func (m *model) startWith(text string, start func(context.Context) (<-chan event.Event, error)) tea.Cmd {
	m.gen++
	ctx, cancel := context.WithCancel(m.ctx)
	stream, err := start(ctx)
	echo := tea.Println(strong.Render("› ") + text)
	if err != nil {
		cancel()
		return tea.Sequence(echo, tea.Println(failed.Render("ycode: "+err.Error())))
	}
	m.running, m.cancel, m.seen, m.activity, m.started = true, cancel, map[uint64]bool{}, "working", time.Now()
	// A cancelled turn may have left a partial line; it is not this turn's.
	m.live.Reset()
	m.streamed = ""
	return tea.Sequence(echo, wait(m.gen, stream, true))
}

func (m *model) interrupt() tea.Cmd {
	if m.interrupted || m.cancel == nil {
		return nil
	}
	m.interrupted = true
	m.activity = "interrupting"
	m.cancel()
	return nil
}

// endTurn settles a cancelled turn and collects what was typed during it.
func (m *model) endTurn() tea.Cmd {
	gen, session, interrupted := m.gen, m.session, m.interrupted
	if m.cancel != nil {
		m.cancel()
	}
	host := m.opts.Host
	return func() tea.Msg {
		if interrupted {
			settle, stop := context.WithTimeout(context.Background(), settleTimeout)
			host.Settle(settle, session)
			stop()
		}
		queued, err := host.TakeQueued(session)
		return turnDoneMsg{gen: gen, queued: queued, err: err}
	}
}

func (m *model) decide(action string) tea.Cmd {
	w := *m.waiting
	m.waiting = nil
	m.activity = "working"
	gen, session, host, ctx := m.gen, m.session, m.opts.Host, m.ctx
	return tea.Sequence(tea.Println(faint.Render("approval: "+action)), func() tea.Msg {
		stream, err := host.Decide(ctx, session, w, action)
		return decideDoneMsg{gen: gen, stream: stream, err: err}
	})
}

func (m *model) slash(s Slash, args []string) tea.Cmd {
	echo := tea.Println(strong.Render(strings.TrimSpace(s.Name + " " + strings.Join(args, " "))))
	switch s.Name {
	case "/quit":
		return tea.Sequence(echo, tea.Println(faint.Render("session "+m.session+" stays resumable")), tea.Quit)
	case "/help":
		var b strings.Builder
		for _, item := range Slashes {
			fmt.Fprintf(&b, "  %-18s %-48s = %s\n", item.Usage, item.Short, item.Command)
		}
		b.WriteString("  Enter sends; during a turn it steers. ESC stops the turn. Tab completes a slash. y/n answers an approval.")
		return tea.Sequence(echo, tea.Println(b.String()))
	}
	m.busy = s.Name
	session, host, ctx := m.session, m.opts.Host, m.ctx
	return tea.Sequence(echo, func() tea.Msg {
		result, err := host.Slash(ctx, session, s, args)
		return slashDoneMsg{session: session, result: result, err: err}
	})
}

func (m *model) refreshStatus() tea.Cmd {
	session, host, epoch := m.session, m.opts.Host, m.epoch
	return func() tea.Msg { return statusMsg{epoch: epoch, status: host.Status(session)} }
}

// replay prints the tail of a session the terminal switched to.
func (m *model) replay() tea.Cmd {
	entries, err := m.opts.Host.Transcript(m.session)
	if err != nil {
		return tea.Println(failed.Render("ycode: " + err.Error()))
	}
	const tail = 6
	var b strings.Builder
	fmt.Fprintf(&b, "resumed session %s (%d messages)", m.session, len(entries))
	if len(entries) > tail {
		entries = entries[len(entries)-tail:]
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "\n%s %s", rolePrefix(e.Role), e.Text)
	}
	return tea.Println(b.String())
}

func rolePrefix(role string) string {
	if role == "user" {
		return strong.Render("›")
	}
	return "●"
}

// render projects one canonical event into the transcript and status.
func (m *model) render(ev event.Event) tea.Cmd {
	data := map[string]any{}
	_ = json.Unmarshal(ev.Data, &data)
	switch ev.Type {
	case "output.emitted":
		var body struct {
			Deliveries []struct {
				PayloadRef string `json:"payload_ref"`
			} `json:"deliveries"`
		}
		_ = json.Unmarshal(ev.Data, &body)
		cmds := []tea.Cmd{m.flushLive()}
		streamed := strings.TrimSpace(m.streamed)
		m.streamed = ""
		for _, delivery := range body.Deliveries {
			payload, err := m.opts.Host.Payload(delivery.PayloadRef)
			if err != nil {
				cmds = append(cmds, tea.Println(failed.Render("ycode: "+err.Error())))
				continue
			}
			if text := strings.TrimRight(string(payload), "\n"); text != "" && strings.TrimSpace(text) != streamed {
				cmds = append(cmds, tea.Println("● "+text))
			}
		}
		return tea.Sequence(cmds...)
	case "llm.delta":
		return m.renderDelta(data)
	case "llm.completed":
		// A tool-call round's text was already shown; the final answer
		// arrives as output.emitted and is not printed twice.
		return m.flushLive()
	case "llm.requested":
		m.streamed = ""
		if model, _ := data["model_ref"].(string); model != "" {
			m.status.Model = model
		}
		m.activity = "thinking"
	case "bashy.run.requested":
		m.activity = "running a command"
	case "bashy.requested":
		if op := summary(data); op != "" {
			m.activity = op
		}
	case "bashy.run.completed":
		m.activity = "working"
	case "hitl.waiting":
		w := Waiting{RunID: ev.RunID}
		w.PolicyRef, _ = data["policy_ref"].(string)
		w.DecisionID, _ = data["decision_id"].(string)
		w.ReviewDigest, _ = data["review_digest"].(string)
		w.ReportDigest, _ = data["report_digest"].(string)
		if v, ok := data["version"].(float64); ok {
			w.Version = uint64(v)
		}
		m.waiting = &w
		m.activity = "waiting for approval"
		return tea.Println(strong.Render("approval needed (" + w.PolicyRef + "): y approve · n reject"))
	case "hitl.resolved":
		m.waiting = nil
	case "turn.failed":
		msg, _ := data["error"].(string)
		if msg == "" {
			msg = "turn failed"
		}
		return tea.Println(failed.Render("✗ " + msg))
	}
	return nil
}

// renderDelta prints streamed answer text a line at a time as the provider
// produces it; the unfinished line is held until its newline or the call ends.
func (m *model) renderDelta(data map[string]any) tea.Cmd {
	if channel, _ := data["channel"].(string); channel != "text" {
		m.activity = "thinking"
		return nil
	}
	// The event carries only a payload_ref; the text lives in the payload
	// store, so resolve it (an unreadable delta is reported, never skipped).
	ref, _ := data["payload_ref"].(string)
	if ref == "" {
		return tea.Println(failed.Render("ycode: llm.delta without payload_ref"))
	}
	raw, err := m.opts.Host.Payload(ref)
	if err != nil {
		return tea.Println(failed.Render("ycode: llm.delta payload: " + err.Error()))
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return tea.Println(failed.Render("ycode: llm.delta payload: " + err.Error()))
	}
	chunk := body.Text
	m.activity = "answering"
	m.live.WriteString(chunk)
	buffered := m.live.String()
	cut := strings.LastIndex(buffered, "\n")
	if cut < 0 {
		return nil
	}
	m.live.Reset()
	m.live.WriteString(buffered[cut+1:])
	return m.printStreamed(buffered[:cut])
}

func (m *model) flushLive() tea.Cmd {
	rest := m.live.String()
	m.live.Reset()
	if rest == "" {
		return nil
	}
	return m.printStreamed(rest)
}

func (m *model) printStreamed(text string) tea.Cmd {
	prefix := "  "
	if m.streamed == "" {
		prefix = "● "
	}
	m.streamed += text + "\n"
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
		prefix = "  "
	}
	return tea.Println(strings.Join(lines, "\n"))
}

func summary(data map[string]any) string {
	for _, key := range []string{"command", "operation", "op"} {
		if v, ok := data[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func (m *model) View() tea.View {
	var b strings.Builder
	switch {
	case m.waiting != nil:
		b.WriteString(strong.Render("approve? y/n") + faint.Render("  (esc stops the turn)") + "\n")
	case m.running:
		b.WriteString(faint.Render("⋯ "+m.activity+"  (enter steers · esc stops)") + "\n")
	case m.busy != "":
		b.WriteString(faint.Render("⋯ "+m.busy) + "\n")
	}
	b.WriteString(m.input.View() + "\n")
	b.WriteString(faint.Render(m.statusLine()))
	return tea.NewView(b.String())
}

func (m *model) statusLine() string {
	id := m.session
	if len(id) > 8 {
		id = id[:8]
	}
	agent := m.opts.Agent
	if m.status.Agent != "" {
		agent = m.status.Agent
	}
	parts := []string{agent}
	if m.status.Model != "" {
		parts = append(parts, m.status.Model)
	}
	if m.status.Selected != "" && m.status.Selected != m.status.Model {
		parts = append(parts, "selected "+m.status.Selected)
	}
	parts = append(parts, "session "+id)
	if m.status.Mode == "plan" {
		parts = append(parts, "plan mode")
	}
	if m.status.ContextTokens > 0 {
		parts = append(parts, fmt.Sprintf("ctx %s tok", humanTokens(m.status.ContextTokens)))
	}
	if m.status.SessionTokens > 0 {
		parts = append(parts, fmt.Sprintf("Σ %s tok", humanTokens(m.status.SessionTokens)))
	}
	if m.status.CostKnown {
		parts = append(parts, fmt.Sprintf("$%.4f", m.status.CostUSD))
	}
	return strings.Join(parts, " · ")
}

func humanTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}
