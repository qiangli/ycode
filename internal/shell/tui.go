package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// dispatchResultMsg carries the outcome of a Dispatch call back to the
// Bubble Tea Update loop so the viewport can be appended to.
type dispatchResultMsg struct {
	stdout string
	stderr string
	result Result
	err    error
}

// shellModel is the Bubble Tea model for the interactive `ycode shell`.
//
// Layout (top-to-bottom):
//
//	┌──── viewport (scrollable, append-only history) ────┐
//	│ ycode shell (skeleton)                             │
//	│ ycode:/cwd$ /help                                  │
//	│ help body…                                         │
//	│ ycode:/cwd$                                        │
//	└────────────────────────────────────────────────────┘
//	┌──── textarea (single-line input) ──────────────────┐
//	│ ycode:/cwd$ │                                      │
//	└────────────────────────────────────────────────────┘
type shellModel struct {
	rt *ShellRuntime
	d  *Dispatcher

	ta textarea.Model
	vp viewport.Model

	width  int
	height int

	historyMu sync.Mutex
	history   []string

	running    bool
	cancelExec context.CancelFunc

	promptStyle lipgloss.Style
	errorStyle  lipgloss.Style
}

// NewShellModel constructs the Bubble Tea model. The runtime must already
// have its slash registry, skill resolver, and TTY runner wired.
func NewShellModel(rt *ShellRuntime) tea.Model {
	ta := textarea.New()
	ta.Placeholder = "type bash, /command, @skill, !agent, ?question…"
	ta.Prompt = ""
	ta.CharLimit = 4096
	ta.SetWidth(80)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Focus()

	vp := viewport.New(80, 20)
	vp.SetContent(welcomeBanner(rt))

	return &shellModel{
		rt:          rt,
		d:           NewDispatcher(rt),
		ta:          ta,
		vp:          vp,
		promptStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true),
		errorStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}
}

// Init runs at startup. No initial command needed.
func (m *shellModel) Init() tea.Cmd { return textarea.Blink }

// Update routes Bubble Tea messages.
func (m *shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ta.SetWidth(msg.Width)
		// Reserve 3 lines for the input row + status.
		vh := msg.Height - 3
		if vh < 5 {
			vh = 5
		}
		m.vp.Width = msg.Width
		m.vp.Height = vh

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			// While a command is running: cancel it. At an idle prompt:
			// clear the current input (classic shell ergonomic). The
			// shell only exits via ^D at an empty prompt or `exit`.
			if m.running && m.cancelExec != nil {
				m.cancelExec()
				return m, nil
			}
			m.ta.Reset()
			m.appendLine(m.shellPrompt() + " ^C")
			return m, nil
		case "ctrl+d":
			if m.ta.Value() == "" {
				return m, tea.Quit
			}
			// ^D while typing — ignore (matches bash with no IGNOREEOF set).
		case "tab":
			if m.running {
				return m, nil
			}
			m.handleTab()
			return m, nil
		case "enter":
			if m.running {
				return m, nil
			}
			input := m.ta.Value()
			m.ta.Reset()
			return m, m.runInput(input)
		}

	case dispatchResultMsg:
		m.running = false
		m.cancelExec = nil
		m.appendOutput(msg)
		// Stay focused on the input.
		m.ta.Focus()
		m.vp.GotoBottom()

	case tuiTTYRequestMsg:
		// A TTY-needing external command (vi, less, top, …) was
		// invoked from inside dispatch. Hand the terminal to the
		// child via tea.ExecProcess; resume the TUI when it exits.
		req := msg
		return m, tea.ExecProcess(req.cmd, func(err error) tea.Msg {
			exit := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exit = exitErr.ExitCode()
				} else {
					exit = 1
				}
			}
			return tuiTTYDoneMsg{resultCh: req.resultCh, exit: exit, err: err}
		})

	case tuiTTYDoneMsg:
		// Unblock the dispatcher goroutine waiting on the runner.
		select {
		case msg.resultCh <- tuiTTYResult{exit: msg.exit, err: msg.err}:
		default:
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	cmds = append(cmds, cmd)
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// View renders the shell.
func (m *shellModel) View() string {
	prompt := m.promptStyle.Render(m.shellPrompt())
	status := ""
	if m.running {
		status = "  (running — ^C to cancel)"
	}
	return strings.Join([]string{
		m.vp.View(),
		"",
		prompt + " " + m.ta.View() + status,
	}, "\n")
}

func (m *shellModel) shellPrompt() string { return fmt.Sprintf("ycode:%s$", m.rt.WorkDir()) }

// runInput dispatches a submitted line in a tea.Cmd goroutine.
func (m *shellModel) runInput(input string) tea.Cmd {
	if strings.TrimSpace(input) == "" {
		// Empty submit — just echo a fresh prompt.
		m.appendLine(m.shellPrompt())
		return nil
	}
	if _, ok := ParseExitCommand(input); ok {
		m.appendLine(m.shellPrompt() + " " + input)
		return tea.Quit
	}
	m.appendLine(m.shellPrompt() + " " + input)
	m.running = true

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelExec = cancel

	intent, classifyErr := Classify(input)
	if classifyErr != nil {
		cancel()
		m.cancelExec = nil
		m.running = false
		m.appendLine(m.errorStyle.Render("shell: " + classifyErr.Error()))
		m.vp.GotoBottom()
		return nil
	}

	return func() tea.Msg {
		var stdout, stderr bytes.Buffer
		sink := WriterSink{StdoutW: &stdout, StderrW: &stderr}
		res, err := m.d.Dispatch(ctx, intent, sink)
		return dispatchResultMsg{
			stdout: stdout.String(),
			stderr: stderr.String(),
			result: res,
			err:    err,
		}
	}
}

func (m *shellModel) appendOutput(msg dispatchResultMsg) {
	if s := strings.TrimRight(msg.stdout, "\n"); s != "" {
		m.appendLine(s)
	}
	if s := strings.TrimRight(msg.stderr, "\n"); s != "" {
		m.appendLine(m.errorStyle.Render(s))
	}
	if msg.err != nil {
		m.appendLine(m.errorStyle.Render("shell: dispatch error: " + msg.err.Error()))
	}
}

// handleTab fires CompleteFor on the current input prefix and either
// auto-completes when there's a unique match or dumps the list of
// candidates into the viewport.
func (m *shellModel) handleTab() {
	prefix := m.ta.Value()
	cs := CompleteFor(m.rt, prefix)
	if len(cs) == 0 {
		m.appendLine("(no completions for " + strconvSafe(prefix) + ")")
		return
	}
	if len(cs) == 1 {
		m.ta.SetValue(cs[0].Replacement)
		m.ta.SetCursor(len(cs[0].Replacement))
		return
	}
	// Multiple candidates → dump to viewport, leave input alone.
	m.appendLine(m.shellPrompt() + " " + prefix + "\t")
	m.appendLine(FormatCompletions(cs))
}

func strconvSafe(s string) string {
	if s == "" {
		return "<empty>"
	}
	return s
}

func (m *shellModel) appendLine(line string) {
	m.historyMu.Lock()
	m.history = append(m.history, line)
	m.historyMu.Unlock()
	m.vp.SetContent(strings.Join(m.history, "\n"))
	m.vp.GotoBottom()
}

func welcomeBanner(rt *ShellRuntime) string {
	return strings.Join([]string{
		"ycode shell — bash + agentic sentinels (skeleton)",
		"  /<word>  slash command (try /help)",
		"  @<id>    skill from registry",
		"  !<text>  agent shot (stub)",
		"  ?<text>  agent Q&A (stub)",
		"  ^C       cancel running command   ^D  exit",
		"  Tab      complete based on prefix (/, @, or PATH)",
		"",
		fmt.Sprintf("cwd: %s", rt.WorkDir()),
		"",
	}, "\n")
}
