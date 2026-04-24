package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/builtin"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/git"
	"github.com/qiangli/ycode/internal/runtime/prompt"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
	"github.com/qiangli/ycode/internal/runtime/usage"
)

// Layout constants.
const (
	statusBarHeight = 1
	textareaHeight  = 3
)

// agentClient is the interface for sending messages through the service layer.
// Satisfied by client.InProcessClient and other client implementations.
type agentClient interface {
	SendMessage(ctx context.Context, sessionID string, input bus.MessageInput) error
	CancelTurn(ctx context.Context, sessionID string) error
	Events(ctx context.Context, filter ...bus.EventType) (<-chan bus.Event, error)
}

// TUIModel is the top-level bubbletea model for interactive mode.
type TUIModel struct {
	app         *App
	cl          agentClient // optional: when set, agentic loop runs through client/service/bus
	viewport    viewport.Model
	textarea    textarea.Model
	width       int
	height      int
	output      strings.Builder
	outputDirty bool // true when output changed since last syncViewport
	ready       bool

	// Agent state.
	working      bool // true while the agent is processing (turn + tools)
	workCancel   context.CancelFunc
	turnMessages []api.Message // accumulated conversation for current agent loop

	// Persistent input history for up/down navigation (cross-session).
	history *promptHistory

	// Confirmation dialog state.
	confirming    bool   // true when waiting for user confirmation
	confirmPrompt string // the question being asked
	confirmYes    func() tea.Cmd
	confirmNo     func() tea.Cmd

	// Permission prompt state (for tool invocations that need approval).
	permChan        chan bool // non-nil when a tool is waiting for permission approval
	permAlwaysAllow bool      // true when user chose "always allow" for this session

	// Parallel tool execution progress.
	toolTasks []toolTaskProgress // per-tool status during parallel execution
	program   *tea.Program       // set after NewProgram; used to send progress msgs

	// Slash command completion.
	completion    completionState  // popup state
	completionAll []completionItem // all available commands (built once)

	// Model picker overlay.
	modelPicker modelPickerState

	// Command palette overlay.
	cmdPalette commandPaletteState

	// Toast notification stack.
	toasts toastState

	// Pending input queue — messages submitted while the agent is working.
	pendingInputs []pendingInput

	// Side query state.
	sideQueryCount int // for numbering /btw output sections
}

// toolTaskProgress tracks a single tool's execution state.
type toolTaskProgress struct {
	Name   string
	Detail string
	Status taskqueue.TaskStatus
}

// Custom message types.

type commandOutputMsg struct {
	Echo        string
	Text        string
	Err         error
	AgentPrompt string // if non-empty, start an agentic turn after displaying Text
}

// turnResultMsg is sent when a conversation turn completes.
type turnResultMsg struct {
	Result      *conversation.TurnResult
	Recovery    *conversation.RecoveryResult
	Err         error
	ToolResults []api.ContentBlock // tool results from preceding tool execution, if any
	Streamed    bool               // true if text deltas were already sent to viewport
}

// toolProgressMsg reports a tool's status change during parallel execution.
type toolProgressMsg taskqueue.TaskEvent

// repaintMsg triggers one more Update/View cycle to flush rendering.
type repaintMsg struct{}

// busEventMsg wraps a bus.Event for delivery through bubbletea's message system.
type busEventMsg struct{ bus.Event }

// streamDeltaMsg delivers a streaming delta from the LLM in the direct path.
type streamDeltaMsg struct {
	EventType string // "text.delta", "thinking.delta", "tool_use.start"
	Text      string // delta text for text/thinking events
	ToolName  string // tool name for tool_use.start
}

// pendingInput is a message submitted while the agent is working.
type pendingInput struct {
	Text string
}

// sideQueryResultMsg is sent when a /btw side query completes.
type sideQueryResultMsg struct {
	Query  string
	Result string
	Err    error
	ID     int
}

// NewTUIModel creates the composite TUI model.
func NewTUIModel(app *App) *TUIModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message or /command..."
	ta.SetHeight(textareaHeight)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // unlimited

	// Customize textarea key bindings: Enter submits, no newlines in input.
	ta.KeyMap.InsertNewline.SetEnabled(false)

	// Determine history storage directory from user config path.
	var historyDir string
	if app.userConfigPath != "" {
		historyDir = filepath.Dir(app.userConfigPath)
	}

	m := &TUIModel{
		app:           app,
		textarea:      ta,
		history:       newPromptHistory(historyDir),
		completionAll: buildCompletionItems(app.commands, app.workDir),
	}

	return m
}

// SetProgram sets the tea.Program and configures the progress callback.
func (m *TUIModel) SetProgram(p *tea.Program) {
	m.program = p
	// Set up progress callback to send messages to the TUI.
	m.app.SetProgressFunc(func(message string) {
		if m.program != nil {
			m.program.Send(progressMsg{message: message})
		}
	})
	m.app.SetDeltaFunc(func(text string) {
		if m.program != nil {
			m.program.Send(commandDeltaMsg{text: text})
		}
	})
}

// progressMsg is sent to show a progress update during command execution.
type progressMsg struct {
	message string
}

// commandDeltaMsg is sent to stream a text delta during command execution.
// Unlike progressMsg, no trailing newline is appended.
type commandDeltaMsg struct {
	text string
}

func (m *TUIModel) Init() tea.Cmd {
	return tea.Batch(m.textarea.Focus(), tea.SetWindowTitle(appTitle))
}

func (m *TUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case progressMsg:
		// Show progress message during command execution.
		m.appendOutput(msg.message + "\n")
		return m, nil

	case commandDeltaMsg:
		// Stream text delta during command execution (no trailing newline).
		m.appendOutput(msg.text)
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		vpHeight := max(m.height-statusBarHeight-textareaHeight-1, 1)

		if !m.ready {
			m.viewport = viewport.New(m.width, vpHeight)
			m.viewport.SetContent(m.welcomeText())
			m.output.WriteString(m.welcomeText())
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = vpHeight
			m.outputDirty = true
			m.flushViewport()
		}
		m.textarea.SetWidth(m.width)

	case tea.KeyMsg:
		// Handle confirmation dialog input first.
		if m.confirming {
			return m.handleConfirmKey(msg)
		}

		// Handle model picker overlay.
		if m.modelPicker.visible {
			switch msg.Type {
			case tea.KeyEsc:
				m.modelPicker.close()
				return m, nil
			case tea.KeyEnter:
				if model := m.modelPicker.selectedModel(); model != "" {
					m.modelPicker.close()
					result, err := m.app.SwitchModel(model)
					if err != nil {
						m.toasts.add(fmt.Sprintf("Model switch failed: %v", err), ToastError)
					} else {
						m.toasts.add(result, ToastSuccess)
					}
					return m, func() tea.Msg { return repaintMsg{} }
				}
			case tea.KeyUp:
				m.modelPicker.moveUp()
				return m, nil
			case tea.KeyDown:
				m.modelPicker.moveDown()
				return m, nil
			case tea.KeyBackspace:
				m.modelPicker.backspace()
				return m, nil
			default:
				if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
					m.modelPicker.typeChar(msg.Runes[0])
					return m, nil
				}
			}
			return m, nil
		}

		// Handle command palette overlay.
		if m.cmdPalette.visible {
			switch msg.Type {
			case tea.KeyEsc:
				m.cmdPalette.close()
				return m, nil
			case tea.KeyEnter:
				if item := m.cmdPalette.selectedItem(); item != nil {
					m.cmdPalette.close()
					name := item.Name
					if strings.HasPrefix(name, "/") {
						// Execute as slash command.
						return m, m.handleInput(name)
					}
					// Handle built-in actions.
					switch name {
					case "Switch Model":
						m.modelPicker.open(m.app.Model())
					case "Toggle Mode":
						return m, m.toggleMode()
					}
					return m, func() tea.Msg { return repaintMsg{} }
				}
			case tea.KeyUp:
				m.cmdPalette.moveUp()
				return m, nil
			case tea.KeyDown:
				m.cmdPalette.moveDown()
				return m, nil
			case tea.KeyBackspace:
				m.cmdPalette.backspace()
				return m, nil
			default:
				if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
					m.cmdPalette.typeChar(msg.Runes[0])
					return m, nil
				}
			}
			return m, nil
		}

		// Handle completion popup keys when visible.
		if m.completion.visible {
			switch msg.Type {
			case tea.KeyTab:
				if name := m.completion.selectedName(); name != "" {
					m.textarea.Reset()
					m.textarea.SetValue("/" + name + " ")
					m.completion.dismiss()
					return m, nil
				}
			case tea.KeyUp:
				m.completion.moveUp()
				return m, nil
			case tea.KeyDown:
				m.completion.moveDown()
				return m, nil
			case tea.KeyEsc:
				m.completion.dismiss()
				return m, nil
			case tea.KeyEnter:
				// Accept selected completion on Enter, then submit.
				if name := m.completion.selectedName(); name != "" {
					m.textarea.Reset()
					text := "/" + name
					m.textarea.SetValue(text)
					m.completion.dismiss()
					// Fall through to submit logic below.
				}
			}
		}

		switch {
		case msg.Type == tea.KeyCtrlC:
			if m.working && m.workCancel != nil {
				m.workCancel()
				m.working = false
				m.workCancel = nil
				m.pendingInputs = nil
				m.appendOutput("\n⏹ Cancelled.\n\n")
				cmds = append(cmds, func() tea.Msg { return repaintMsg{} })
				break
			}
			return m, tea.Quit
		case msg.Type == tea.KeyCtrlD:
			return m, tea.Quit
		case msg.Type == tea.KeyShiftTab:
			return m, m.toggleMode()
		case msg.Type == tea.KeyCtrlK:
			m.cmdPalette.open(m.buildPaletteItems())
			return m, nil
		case msg.Type == tea.KeyUp:
			if val, ok := m.history.Up(m.textarea.Value()); ok {
				m.textarea.SetValue(val)
				m.textarea.CursorEnd()
			}
			return m, nil
		case msg.Type == tea.KeyDown:
			if val, ok := m.history.Down(); ok {
				m.textarea.SetValue(val)
				m.textarea.CursorEnd()
			}
			return m, nil
		case msg.Type == tea.KeyEnter:
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				break
			}
			m.completion.dismiss()
			m.history.Append(text)
			m.textarea.Reset()
			// /quit and /exit always take effect immediately.
			if text == "/quit" || text == "/exit" {
				return m, tea.Quit
			}
			if m.working {
				// While agent is working: /btw runs a side query, anything else is queued.
				if strings.HasPrefix(text, "/btw ") {
					query := strings.TrimSpace(text[5:])
					if query != "" {
						return m, m.startSideQuery(query)
					}
					break
				}
				m.pendingInputs = append(m.pendingInputs, pendingInput{Text: text})
				m.appendOutput(fmt.Sprintf("⏳ Queued: %s\n", text))
				break
			}
			return m, m.handleInput(text)
		}

	case permissionRequestMsg:
		// If user previously chose "always allow", auto-approve.
		if m.permAlwaysAllow {
			msg.ReplyCh <- true
			return m, nil
		}
		// A tool is requesting elevated permission — show confirmation dialog.
		m.confirming = true
		m.confirmPrompt = fmt.Sprintf("Allow tool %q (%s)? (y/n/a)", msg.ToolName, msg.RequiredMode)
		m.permChan = msg.ReplyCh
		m.confirmYes = func() tea.Cmd {
			if m.permChan != nil {
				m.permChan <- true
				m.permChan = nil
			}
			return func() tea.Msg { return repaintMsg{} }
		}
		m.confirmNo = func() tea.Cmd {
			if m.permChan != nil {
				m.permChan <- false
				m.permChan = nil
			}
			return func() tea.Msg { return repaintMsg{} }
		}
		return m, nil

	case turnResultMsg:
		if msg.Err != nil {
			m.working = false
			m.workCancel = nil
			m.pendingInputs = nil // clear queue on error
			// Check if it was a cancellation.
			if msg.Err.Error() == "turn 1: stream: context canceled" || strings.Contains(msg.Err.Error(), "context canceled") {
				m.appendOutput("\n⏹ Cancelled.\n\n")
			} else {
				m.appendOutput(fmt.Sprintf("\n✗ Error: %v\n\n", msg.Err))
			}
			cmds = append(cmds, func() tea.Msg { return repaintMsg{} })
			break
		}

		// Show recovery info if compaction occurred
		if msg.Recovery != nil && msg.Recovery.RetrySuccessful {
			m.appendOutput(fmt.Sprintf("\n⚠ Context compacted: %d messages summarized, %d recent messages preserved.\n\n",
				msg.Recovery.CompactedCount, msg.Recovery.PreservedCount))
		}

		// If this turn was preceded by tool execution, append the tool results
		// to the conversation history so subsequent turns see the full sequence.
		if len(msg.ToolResults) > 0 {
			m.turnMessages = append(m.turnMessages, api.Message{
				Role:    api.RoleUser,
				Content: msg.ToolResults,
			})
		}

		result := msg.Result

		// Track usage from this turn.
		m.app.usageTracker.Add(
			result.Usage.InputTokens,
			result.Usage.OutputTokens,
			result.Usage.CacheCreationInput,
			result.Usage.CacheReadInput,
		)

		// Display LLM call metrics.
		m.appendOutput(formatLLMMetrics(result))

		// Display text output from the model.
		// When Streamed is true, text was already rendered via streamDeltaMsg.
		if result.TextContent != "" {
			if !msg.Streamed {
				m.appendOutput(result.TextContent)
			}

			// Save assistant response to session.
			_ = m.app.session.AddMessage(session.ConversationMessage{
				Role: session.RoleAssistant,
				Content: []session.ContentBlock{
					{Type: session.ContentTypeText, Text: result.TextContent},
				},
			})
		}

		// If no tool calls, turn is complete.
		if len(result.ToolCalls) == 0 {
			m.working = false
			m.workCancel = nil
			m.appendOutput("\n✓ Done.\n\n")
			// Show session summary.
			m.appendOutput(formatSessionSummary(m.app.usageTracker, m.app.sessionStart))
			m.appendOutput("\n")
			// Drain pending input queue or go idle.
			if cmd := m.drainPendingInput(); cmd != nil {
				cmds = append(cmds, cmd)
			} else {
				cmds = append(cmds, func() tea.Msg { return repaintMsg{} })
				cmds = append(cmds, alertDone())
			}
			break
		}

		// Show tool calls with descriptive progress.
		if len(result.ToolCalls) > 1 {
			m.appendOutput(fmt.Sprintf("\n⚙ Executing %d tools in parallel...\n", len(result.ToolCalls)))
			m.toolTasks = make([]toolTaskProgress, len(result.ToolCalls))
			for i, tc := range result.ToolCalls {
				detail := toolDetail(tc.Name, tc.Input)
				m.toolTasks[i] = toolTaskProgress{Name: tc.Name, Detail: detail, Status: taskqueue.StatusQueued}
				m.appendOutput(fmt.Sprintf("  ◻ %s\n", detail))
			}
		} else {
			for _, tc := range result.ToolCalls {
				detail := toolDetail(tc.Name, tc.Input)
				m.appendOutput(fmt.Sprintf("\n⚙ %s\n", detail))
			}
			m.toolTasks = nil
		}

		// Build assistant message with tool_use blocks for conversation history.
		var assistantBlocks []api.ContentBlock
		if result.ThinkingContent != "" {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type: api.ContentTypeThinking,
				Text: result.ThinkingContent,
			})
		}
		if result.TextContent != "" {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type: api.ContentTypeText,
				Text: result.TextContent,
			})
		}
		for _, tc := range result.ToolCalls {
			assistantBlocks = append(assistantBlocks, api.ContentBlock{
				Type:  api.ContentTypeToolUse,
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			})
		}
		m.turnMessages = append(m.turnMessages, api.Message{
			Role:    api.RoleAssistant,
			Content: assistantBlocks,
		})
		m.appendOutput("⧗ Sending tool results to LLM...\n")

		// Execute tools and feed results back (in a Cmd to keep TUI responsive).
		toolCalls := result.ToolCalls
		return m, m.executeToolsCmd(toolCalls)

	case toolProgressMsg:
		if int(msg.Index) < len(m.toolTasks) {
			m.toolTasks[msg.Index].Status = msg.Status
			icon := "◻"
			switch msg.Status {
			case taskqueue.StatusRunning:
				icon = "⧗"
			case taskqueue.StatusCompleted:
				icon = "✓"
			case taskqueue.StatusFailed:
				icon = "✗"
			}
			m.appendOutput(fmt.Sprintf("  %s %s\n", icon, m.toolTasks[msg.Index].Detail))
		}
		return m, nil

	case commandOutputMsg:
		m.appendOutput(msg.Echo)
		if msg.Err != nil {
			m.working = false
			m.appendOutput(fmt.Sprintf("Error: %v\n\n", msg.Err))
			cmds = append(cmds, func() tea.Msg { return repaintMsg{} })
		} else {
			if msg.Text != "" {
				m.appendOutput(msg.Text + "\n\n")
			}
			if msg.AgentPrompt != "" {
				// Chain into an agentic turn (e.g. /init scaffold → LLM enhancement).
				cmds = append(cmds, m.startAgentTurn(msg.AgentPrompt))
			} else {
				m.working = false
				// Drain pending input queue or go idle.
				if cmd := m.drainPendingInput(); cmd != nil {
					cmds = append(cmds, cmd)
				} else {
					cmds = append(cmds, func() tea.Msg { return repaintMsg{} })
				}
			}
		}

	case streamDeltaMsg:
		switch msg.EventType {
		case "text.delta", "thinking.delta":
			m.appendOutput(msg.Text)
		case "tool_use.start":
			m.appendOutput(fmt.Sprintf("\n⚙ Tool(%s)\n", msg.ToolName))
		}
		return m, nil

	case sideQueryResultMsg:
		if msg.Err != nil {
			m.appendOutput(fmt.Sprintf("└─ BTW #%d Error: %v\n\n", msg.ID, msg.Err))
		} else {
			// Render with left-border prefix for visual separation.
			lines := strings.Split(strings.TrimRight(msg.Result, "\n"), "\n")
			for _, line := range lines {
				m.appendOutput(fmt.Sprintf("│  %s\n", line))
			}
			m.appendOutput(fmt.Sprintf("└─ BTW #%d done.\n\n", msg.ID))
		}
		return m, nil

	case busEventMsg:
		return m.handleBusEvent(msg.Event)

	case alertDoneMsg:
		m.handleAlertDone()

	case repaintMsg:
		// No-op; triggers Update/View cycle.
	}

	// Update sub-components — textarea is always active so users can type
	// while the agent is working.
	{
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)

		// Update completion state after textarea processes the keystroke.
		m.completion.update(m.completionAll, m.textarea.Value())
	}

	// Forward messages to the viewport for scrolling support.
	// Block most key presses so typing in the textarea doesn't trigger
	// viewport shortcuts (e.g. space → page-down), but allow dedicated
	// scroll keys (PageUp/PageDown) through so users can review output.
	forwardToViewport := true
	if keyMsg, isKey := msg.(tea.KeyMsg); isKey {
		switch keyMsg.Type {
		case tea.KeyPgUp, tea.KeyPgDown:
			forwardToViewport = true
		default:
			forwardToViewport = false
		}
	}
	if forwardToViewport {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *TUIModel) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Prune expired toasts.
	m.toasts.prune()

	// Check for overlays (command palette, model picker).
	if m.cmdPalette.visible {
		overlay := renderCommandPalette(&m.cmdPalette, m.width)
		return fmt.Sprintf("%s\n%s\n%s\n%s",
			m.viewport.View(),
			m.statusBar(),
			overlay,
			m.textarea.View(),
		)
	}

	if m.modelPicker.visible {
		overlay := renderModelPicker(&m.modelPicker, m.width, m.height)
		return fmt.Sprintf("%s\n%s\n%s\n%s",
			m.viewport.View(),
			m.statusBar(),
			overlay,
			m.textarea.View(),
		)
	}

	popup := renderCompletion(&m.completion, m.width)
	if popup != "" {
		return fmt.Sprintf("%s\n%s\n%s\n%s",
			m.viewport.View(),
			m.statusBar(),
			popup,
			m.textarea.View(),
		)
	}

	// Toast notifications overlay on the viewport.
	toastOverlay := renderToasts(&m.toasts, m.width)
	if toastOverlay != "" {
		return fmt.Sprintf("%s\n%s\n%s\n%s",
			m.viewport.View(),
			toastOverlay,
			m.statusBar(),
			m.textarea.View(),
		)
	}

	return fmt.Sprintf("%s\n%s\n%s",
		m.viewport.View(),
		m.statusBar(),
		m.textarea.View(),
	)
}

// statusBar renders the mode and model indicator bar.
func (m *TUIModel) statusBar() string {
	width := m.width
	if width < 10 {
		width = 80
	}

	// Mode indicator.
	modeText := " BUILD "
	modeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#34d399")) // green
	if m.app.InPlanMode() {
		modeText = " PLAN "
		modeStyle = modeStyle.Background(lipgloss.Color("#fbbf24")) // yellow
	}
	if m.working {
		modeText = " WORKING "
		modeStyle = modeStyle.Background(lipgloss.Color("#60a5fa")) // blue
	}
	if m.confirming {
		modeText = " CONFIRM "
		modeStyle = modeStyle.Background(lipgloss.Color("#f472b6")) // pink
	}
	if !m.working && !m.confirming {
		input := strings.TrimSpace(m.textarea.Value())
		if strings.HasPrefix(input, "!!") {
			modeText = " TTY "
			modeStyle = modeStyle.Background(lipgloss.Color("#e879f9")) // magenta
		} else if strings.HasPrefix(input, "!") {
			modeText = " SHELL "
			modeStyle = modeStyle.Background(lipgloss.Color("#f97316")) // orange
		}
	}
	mode := modeStyle.Render(modeText)

	// Model info.
	modelText := fmt.Sprintf(" %s (%s) ", m.app.Model(), m.app.ProviderKind())
	modelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a3a3a3"))
	model := modelStyle.Render(modelText)

	// Usage info (tokens + cost).
	tracker := m.app.UsageTracker()
	usageText := ""
	if tracker.TotalRequests > 0 {
		totalTokens := tracker.TotalTokens()
		cost := tracker.Cost()
		if cost >= 0.01 {
			usageText = fmt.Sprintf(" %dk tokens | $%.2f ", totalTokens/1000, cost)
		} else {
			usageText = fmt.Sprintf(" %dk tokens | $%.4f ", totalTokens/1000, cost)
		}
	}
	usageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#818cf8")) // indigo
	usageInfo := usageStyle.Render(usageText)

	// Session info.
	sessionText := ""
	if sid := m.app.SessionID(); sid != "" {
		title := m.app.session.Title
		if title != "" {
			if len(title) > 20 {
				title = title[:17] + "..."
			}
			sessionText = fmt.Sprintf(" [%s] ", title)
		} else {
			short := sid
			if len(short) > 8 {
				short = short[:8]
			}
			sessionText = fmt.Sprintf(" [%s] ", short)
		}
	}
	sessionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#737373"))
	sessionInfo := sessionStyle.Render(sessionText)

	// Hints.
	hintText := " ctrl+k:commands | shift+tab:mode | /help "
	if m.confirming {
		hintText = " " + m.confirmPrompt + "  y=yes n=no a=always for session "
	}
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#737373"))
	hint := hintStyle.Render(hintText)

	// Fill bar.
	barStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#262626"))

	left := mode + model + usageInfo + sessionInfo
	right := hint

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	fillWidth := width - leftWidth - rightWidth
	if fillWidth < 0 {
		fillWidth = 0
	}
	fill := barStyle.Render(strings.Repeat(" ", fillWidth))

	return left + fill + right
}

// welcomeText returns the splash screen with ASCII banner and context info.
func (m *TUIModel) welcomeText() string {
	var b strings.Builder

	// Styles.
	logoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6d28d9"))   // purple
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")) // light purple
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))  // dim
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5e5e5"))  // bright
	hintBold := lipgloss.NewStyle().Foreground(lipgloss.Color("#a78bfa")).Bold(true)
	hintDim := lipgloss.NewStyle().Foreground(lipgloss.Color("#737373"))

	// ASCII banner.
	banner := []string{
		`██╗   ██╗ ██████╗ ██████╗ ██████╗ ███████╗`,
		`╚██╗ ██╔╝██╔════╝██╔═══██╗██╔══██╗██╔════╝`,
		` ╚████╔╝ ██║     ██║   ██║██║  ██║█████╗  `,
		`  ╚██╔╝  ██║     ██║   ██║██║  ██║██╔══╝  `,
		`   ██║   ╚██████╗╚██████╔╝██████╔╝███████╗`,
		`   ╚═╝    ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝`,
	}
	for _, line := range banner {
		b.WriteString(logoStyle.Render(line))
		b.WriteByte('\n')
	}
	b.WriteString(accentStyle.Render("  autonomous agent harness"))
	b.WriteString("\n\n")

	// Context info.
	type infoLine struct {
		label string
		value string
	}

	// Gather git context.
	gitCtx := git.Discover(m.app.workDir)
	branch := "n/a"
	workspace := "n/a"
	if gitCtx.IsRepo {
		branch = gitCtx.Branch
		if gitCtx.Status != "" {
			lines := strings.Split(gitCtx.Status, "\n")
			if len(lines) <= 1 {
				workspace = "clean"
			} else {
				workspace = fmt.Sprintf("%d changed", len(lines)-1)
			}
		} else {
			workspace = "clean"
		}
	}

	permMode := m.app.config.PermissionMode
	if permMode == "" {
		permMode = "ask"
	}

	info := []infoLine{
		{"Model", fmt.Sprintf("%s via %s", m.app.Model(), m.app.ProviderKind())},
		{"Permissions", permMode},
		{"Branch", branch},
		{"Workspace", workspace},
		{"Directory", m.app.workDir},
		{"Session", m.app.session.ID},
		{"Version", m.app.version},
	}

	maxLabel := 0
	for _, i := range info {
		if len(i.label) > maxLabel {
			maxLabel = len(i.label)
		}
	}

	for _, i := range info {
		padded := i.label + strings.Repeat(" ", maxLabel-len(i.label))
		b.WriteString("  ")
		b.WriteString(labelStyle.Render(padded))
		b.WriteString("  ")
		b.WriteString(valueStyle.Render(i.value))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	// Help hints.
	b.WriteString("  ")
	b.WriteString(hintBold.Render("/help"))
	b.WriteString(hintDim.Render(" for commands"))
	b.WriteString(hintDim.Render(" · "))
	b.WriteString(hintBold.Render("/model"))
	b.WriteString(hintDim.Render(" to switch models"))
	b.WriteString(hintDim.Render(" · "))
	b.WriteString(hintBold.Render("shift+tab"))
	b.WriteString(hintDim.Render(" to toggle mode"))
	b.WriteString("\n\n")

	return b.String()
}

// appendOutput adds text to the output buffer and refreshes the viewport.
func (m *TUIModel) appendOutput(text string) {
	m.output.WriteString(text)
	m.outputDirty = true
	m.flushViewport()
}

// flushViewport word-wraps the output and updates the viewport content.
// Only performs work when the output has changed (dirty flag) or on resize.
func (m *TUIModel) flushViewport() {
	if !m.outputDirty {
		return
	}
	m.outputDirty = false
	width := m.viewport.Width
	if width < 1 {
		width = 80
	}
	wrapped := ansi.Wrap(m.output.String(), width, "")
	m.viewport.SetContent(wrapped)
	m.viewport.GotoBottom()
}

// handleInput dispatches text as either a slash command or a prompt.
func (m *TUIModel) handleInput(text string) tea.Cmd {
	// Save user message to session.
	_ = m.app.session.AddMessage(session.ConversationMessage{
		Role: session.RoleUser,
		Content: []session.ContentBlock{
			{Type: session.ContentTypeText, Text: text},
		},
	})

	// Auto-generate session title from first user message.
	if m.app.session.Title == "" && !strings.HasPrefix(text, "/") &&
		!strings.HasPrefix(text, "!") {
		m.app.session.GenerateDefaultTitle()
	}

	if strings.HasPrefix(text, "!!") {
		shell := strings.TrimLeft(text[2:], " ")
		if shell == "" {
			return func() tea.Msg {
				return commandOutputMsg{Echo: fmt.Sprintf("> %s\n", text), Text: "Usage: !! <command>"}
			}
		}
		m.appendOutput(fmt.Sprintf("> %s\n", text))
		cmd := exec.Command("sh", "-c", shell)
		return tea.ExecProcess(cmd, func(err error) tea.Msg {
			if err != nil {
				return commandOutputMsg{Err: err}
			}
			return commandOutputMsg{Text: "(exited)"}
		})
	}

	if strings.HasPrefix(text, "!") {
		shell := strings.TrimLeft(text[1:], " ")
		if shell == "" {
			return func() tea.Msg {
				return commandOutputMsg{Echo: fmt.Sprintf("> %s\n", text), Text: "Usage: ! <command>"}
			}
		}
		echo := fmt.Sprintf("> %s\n", text)
		return func() tea.Msg {
			cmd := exec.Command("sh", "-c", shell)
			out, err := cmd.CombinedOutput()
			return commandOutputMsg{Echo: echo, Text: string(out), Err: err}
		}
	}

	// Side query — works both while idle and while agent is working.
	if strings.HasPrefix(text, "/btw ") {
		query := strings.TrimSpace(text[5:])
		if query != "" {
			m.appendOutput(fmt.Sprintf("> %s\n", text))
			return m.startSideQuery(query)
		}
	}

	if strings.HasPrefix(text, "/") {
		rest := text[1:]
		name, args, _ := strings.Cut(rest, " ")

		if name == "quit" || name == "exit" {
			return tea.Quit
		}

		// Try built-in commands first; fall through to agent for unregistered
		// names (e.g. skill slash commands like /claude, /build).
		if spec, ok := m.app.commands.Get(name); ok {
			// Show the command echo immediately, before starting execution.
			m.appendOutput(fmt.Sprintf("> %s\n", text))
			m.working = true
			promptFn := spec.AgentPrompt
			cmdArgs := args
			return func() tea.Msg {
				output, err := m.app.commands.Execute(context.Background(), name, cmdArgs)
				msg := commandOutputMsg{Text: output, Err: err}
				if promptFn != nil && err == nil {
					msg.AgentPrompt = promptFn(cmdArgs)
				}
				return msg
			}
		}
	}

	// Check for high-confidence builtin intent before the expensive agentic loop.
	if intent := builtin.DetectIntent(text); intent != nil {
		m.appendOutput(fmt.Sprintf("> %s\n", text))
		m.appendOutput("⧗ Running builtin /" + intent.Operation + "...\n")
		m.working = true
		return func() tea.Msg {
			output, err := m.app.commands.Execute(context.Background(), intent.Operation, intent.Args)
			return commandOutputMsg{Text: output, Err: err}
		}
	}

	// Start agentic turn — handles regular prompts and skill slash commands.
	m.appendOutput(fmt.Sprintf("> %s\n", text))
	m.resetTitle()
	return m.startAgentTurn(text)
}

// startAgentTurn begins an agentic conversation turn with system prompt, tools, and history.
func (m *TUIModel) startAgentTurn(userPrompt string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.workCancel = cancel
	m.working = true

	m.appendOutput("⧗ Sending to LLM...\n")

	// Event-driven path: use the client/service layer + bus events.
	if m.cl != nil {
		return m.startAgentTurnViaClient(ctx, userPrompt)
	}

	// Direct path: call App methods directly with streaming.
	m.turnMessages = m.app.sessionMessages()
	m.turnMessages = append(m.turnMessages, api.Message{
		Role: api.RoleUser,
		Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: userPrompt},
		},
	})

	msgs := m.turnMessages
	prog := m.program
	return func() tea.Msg {
		onEvent := func(eventType string, data map[string]any) {
			if prog == nil {
				return
			}
			switch eventType {
			case "text.delta":
				if text, ok := data["text"].(string); ok {
					prog.Send(streamDeltaMsg{EventType: eventType, Text: text})
				}
			case "thinking.delta":
				if text, ok := data["text"].(string); ok {
					prog.Send(streamDeltaMsg{EventType: eventType, Text: text})
				}
			case "tool_use.start":
				if name, ok := data["tool"].(string); ok {
					prog.Send(streamDeltaMsg{EventType: eventType, ToolName: name})
				}
			}
		}
		result, recovery, err := m.app.RunTurnWithRecoveryStreaming(ctx, msgs, onEvent)
		return turnResultMsg{Result: result, Recovery: recovery, Err: err, Streamed: true}
	}
}

// startAgentTurnViaClient sends the message through the service layer and
// listens for bus events, forwarding them to the TUI via program.Send().
func (m *TUIModel) startAgentTurnViaClient(ctx context.Context, userPrompt string) tea.Cmd {
	prog := m.program
	sessionID := m.app.SessionID()

	return func() tea.Msg {
		// Subscribe to events.
		evCh, err := m.cl.Events(ctx)
		if err != nil {
			return turnResultMsg{Err: err}
		}

		// Send message (async — events arrive on evCh).
		go func() {
			if err := m.cl.SendMessage(ctx, sessionID, bus.MessageInput{Text: userPrompt}); err != nil {
				if prog != nil {
					prog.Send(turnResultMsg{Err: err})
				}
			}
		}()

		// Forward bus events to TUI until turn completes.
		for ev := range evCh {
			if prog != nil {
				prog.Send(busEventMsg{ev})
			}
			if ev.Type == bus.EventTurnComplete || ev.Type == bus.EventTurnError {
				return nil // turn is done, stop forwarding
			}
		}
		return nil
	}
}

// executeToolsCmd runs tool calls in a background Cmd and sends the next turn.
func (m *TUIModel) executeToolsCmd(calls []conversation.ToolCall) tea.Cmd {
	// Create a fresh cancellable context for tool execution + next turn.
	ctx, cancel := context.WithCancel(context.Background())
	m.workCancel = cancel

	msgs := m.turnMessages
	prog := m.program
	return func() tea.Msg {
		// Set up progress reporting: forward events to the TUI via program.Send.
		var progressSend chan<- taskqueue.TaskEvent
		var progressCh chan taskqueue.TaskEvent
		if prog != nil && len(calls) > 1 {
			progressCh = make(chan taskqueue.TaskEvent, len(calls)*4)
			progressSend = progressCh
			go func() {
				for ev := range progressCh {
					prog.Send(toolProgressMsg(ev))
				}
			}()
		}

		// Execute tools (parallel if enabled and multiple calls).
		toolResults := m.app.ExecuteTools(ctx, calls, progressSend)

		// Close progress channel to stop the forwarder goroutine.
		if progressCh != nil {
			close(progressCh)
		}

		// Check for cancellation before sending the next turn.
		if ctx.Err() != nil {
			return turnResultMsg{Err: ctx.Err()}
		}

		// Append tool results to conversation.
		updatedMsgs := append(msgs, api.Message{
			Role:    api.RoleUser,
			Content: toolResults,
		})

		// Run the next turn with tool results (with streaming support).
		onEvent := func(eventType string, data map[string]any) {
			if prog == nil {
				return
			}
			switch eventType {
			case "text.delta":
				if text, ok := data["text"].(string); ok {
					prog.Send(streamDeltaMsg{EventType: eventType, Text: text})
				}
			case "thinking.delta":
				if text, ok := data["text"].(string); ok {
					prog.Send(streamDeltaMsg{EventType: eventType, Text: text})
				}
			case "tool_use.start":
				if name, ok := data["tool"].(string); ok {
					prog.Send(streamDeltaMsg{EventType: eventType, ToolName: name})
				}
			}
		}
		result, recovery, err := m.app.RunTurnWithRecoveryStreaming(ctx, updatedMsgs, onEvent)
		return turnResultMsg{Result: result, Recovery: recovery, Err: err, ToolResults: toolResults, Streamed: true}
	}
}

// startSideQuery runs a lightweight, tool-free LLM query in the background
// without interfering with the main agentic turn. Output is visually boxed.
func (m *TUIModel) startSideQuery(query string) tea.Cmd {
	m.sideQueryCount++
	id := m.sideQueryCount
	m.appendOutput(fmt.Sprintf("\n┌─ BTW #%d: %s\n", id, query))

	return func() tea.Msg {
		msgs := []api.Message{{
			Role: api.RoleUser,
			Content: []api.ContentBlock{
				{Type: api.ContentTypeText, Text: query},
			},
		}}
		// Use a fresh runtime with no event callback — side queries render
		// their result all at once (not streamed) to avoid interleaving.
		result, _, err := m.app.RunTurnWithRecovery(context.Background(), msgs)
		if err != nil {
			return sideQueryResultMsg{Query: query, Err: err, ID: id}
		}
		return sideQueryResultMsg{Query: query, Result: result.TextContent, ID: id}
	}
}

// drainPendingInput dequeues the next pending input and starts it as an
// agentic turn. Returns nil if the queue is empty.
func (m *TUIModel) drainPendingInput() tea.Cmd {
	if len(m.pendingInputs) == 0 {
		return nil
	}
	next := m.pendingInputs[0]
	m.pendingInputs = m.pendingInputs[1:]
	m.appendOutput(fmt.Sprintf("> %s (from queue)\n", next.Text))
	return m.startAgentTurn(next.Text)
}

// toolDetail returns a descriptive label for a tool invocation,
// including relevant parameters like file paths and commands.
func toolDetail(name string, input json.RawMessage) string {
	var params map[string]any
	_ = json.Unmarshal(input, &params)

	str := func(key string) string {
		if v, ok := params[key].(string); ok {
			return v
		}
		return ""
	}

	truncate := func(s string, max int) string {
		s = strings.ReplaceAll(s, "\n", " ")
		if len(s) > max {
			return s[:max-3] + "..."
		}
		return s
	}

	shorten := func(path string) string {
		// Show just the filename for short display; keep relative-style paths.
		return filepath.Base(path)
	}

	switch name {
	case "bash":
		if cmd := str("command"); cmd != "" {
			return fmt.Sprintf("Bash(%s)", truncate(cmd, 100))
		}
		return "Running shell command..."
	case "read_file":
		if fp := str("file_path"); fp != "" {
			return fmt.Sprintf("Read(%s)", shorten(fp))
		}
		return "Reading file..."
	case "write_file":
		if fp := str("file_path"); fp != "" {
			return fmt.Sprintf("Write(%s)", shorten(fp))
		}
		return "Writing file..."
	case "edit_file":
		if fp := str("file_path"); fp != "" {
			return fmt.Sprintf("Edit(%s)", shorten(fp))
		}
		return "Editing file..."
	case "glob_search":
		if pat := str("pattern"); pat != "" {
			return fmt.Sprintf("Glob(%s)", pat)
		}
		return "Searching for files..."
	case "grep_search":
		if pat := str("pattern"); pat != "" {
			return fmt.Sprintf("Grep(%s)", truncate(pat, 60))
		}
		return "Searching file contents..."
	case "WebFetch":
		if url := str("url"); url != "" {
			return fmt.Sprintf("WebFetch(%s)", truncate(url, 80))
		}
		return "Fetching web page..."
	case "WebSearch":
		if q := str("query"); q != "" {
			return fmt.Sprintf("WebSearch(%s)", truncate(q, 60))
		}
		return "Searching the web..."
	case "Agent":
		if desc := str("description"); desc != "" {
			return fmt.Sprintf("Agent(%s)", truncate(desc, 60))
		}
		return "Spawning sub-agent..."
	default:
		return fmt.Sprintf("Tool(%s)", name)
	}
}

// formatLLMMetrics returns a short summary of LLM call duration and token usage.
func formatLLMMetrics(result *conversation.TurnResult) string {
	dur := result.Duration.Seconds()
	total := result.Usage.InputTokens + result.Usage.OutputTokens
	if total == 0 && dur < 0.01 {
		return ""
	}
	return fmt.Sprintf("  ↳ %.1fs | %s tokens in, %s tokens out\n",
		dur, formatTokenCount(result.Usage.InputTokens), formatTokenCount(result.Usage.OutputTokens))
}

// formatTokenCount formats a token count with k suffix for readability.
func formatTokenCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// formatSessionSummary returns a formatted session summary with total time, tokens, and estimated cost.
func formatSessionSummary(tracker *usage.Tracker, startTime time.Time) string {
	duration := time.Since(startTime)
	totalTokens := tracker.InputTokens + tracker.OutputTokens
	cost := tracker.Cost()
	return fmt.Sprintf("  Session: %s | %s in, %s out | %s total | ~$%.4f",
		formatDuration(duration),
		formatTokenCount(tracker.InputTokens),
		formatTokenCount(tracker.OutputTokens),
		formatTokenCount(totalTokens),
		cost)
}

// toggleMode switches between plan and build mode.
// Both directions are immediate — no confirmation needed.
// A transition reminder is injected into the session so the LLM
// is aware of the mode change on the next turn.
func (m *TUIModel) toggleMode() tea.Cmd {
	if m.app.planMode == nil {
		return func() tea.Msg {
			return commandOutputMsg{Text: "Mode switching not available (no .agents/ycode/ directory)"}
		}
	}

	if m.app.InPlanMode() {
		return func() tea.Msg {
			result, err := m.app.planMode.ExitPlanMode()
			if err == nil {
				m.injectModeTransition(prompt.BuildTransitionReminder())
			}
			return commandOutputMsg{Text: result, Err: err}
		}
	}

	return func() tea.Msg {
		result, err := m.app.planMode.EnterPlanMode()
		if err == nil {
			m.injectModeTransition(prompt.PlanTransitionReminder())
		}
		return commandOutputMsg{Text: result, Err: err}
	}
}

// injectModeTransition adds a system reminder message to the session
// so the LLM is informed about the mode change on its next turn.
func (m *TUIModel) injectModeTransition(reminder string) {
	msg := session.ConversationMessage{
		Role: session.RoleUser,
		Content: []session.ContentBlock{
			{Type: session.ContentTypeText, Text: reminder},
		},
	}
	_ = m.app.session.AddMessage(msg)
}

// handleBusEvent processes events from the service layer bus.
// This is the event-driven rendering path used when cl (agentClient) is set.
func (m *TUIModel) handleBusEvent(ev bus.Event) (tea.Model, tea.Cmd) {
	var data map[string]any
	_ = json.Unmarshal(ev.Data, &data)

	str := func(key string) string {
		if v, ok := data[key].(string); ok {
			return v
		}
		return ""
	}

	switch ev.Type {
	case bus.EventTurnStart:
		// Already showing "Sending to LLM..." from startAgentTurn.

	case bus.EventTextDelta:
		if text := str("text"); text != "" {
			m.appendOutput(text)
		}

	case bus.EventThinkingDelta:
		if text := str("text"); text != "" {
			m.appendOutput(text) // thinking content rendered inline
		}

	case bus.EventToolUseStart:
		tool := str("tool")
		m.appendOutput(fmt.Sprintf("\n⚙ Tool(%s)\n", tool))

	case bus.EventToolProgress:
		tool := str("tool")
		status := str("status")
		icon := "⧗"
		switch status {
		case "completed":
			icon = "✓"
		case "failed":
			icon = "✗"
		case "queued":
			icon = "◻"
		}
		m.appendOutput(fmt.Sprintf("  %s %s\n", icon, tool))

	case bus.EventToolResult:
		// Tool result handled — will be followed by next turn or turn.complete.

	case bus.EventTurnComplete:
		m.working = false
		m.workCancel = nil
		m.appendOutput("\n✓ Done.\n\n")
		m.appendOutput(formatSessionSummary(m.app.usageTracker, m.app.sessionStart))
		m.appendOutput("\n")
		// Drain pending input queue or go idle.
		if cmd := m.drainPendingInput(); cmd != nil {
			return m, cmd
		}
		return m, tea.Batch(func() tea.Msg { return repaintMsg{} }, alertDone())

	case bus.EventTurnError:
		m.working = false
		m.workCancel = nil
		m.pendingInputs = nil // clear queue on error
		errMsg := str("error")
		if strings.Contains(errMsg, "context canceled") {
			m.appendOutput("\n⏹ Cancelled.\n\n")
		} else {
			m.appendOutput(fmt.Sprintf("\n✗ Error: %s\n\n", errMsg))
		}
		return m, func() tea.Msg { return repaintMsg{} }

	case bus.EventUsageUpdate:
		if in, ok := data["input_tokens"].(float64); ok {
			if out, ok := data["output_tokens"].(float64); ok {
				m.app.usageTracker.Add(int(in), int(out), 0, 0)
			}
		}

	case bus.EventPermissionReq:
		reqID := str("request_id")
		toolName := str("tool")
		if m.permAlwaysAllow {
			m.appendOutput(fmt.Sprintf("  Auto-allowing tool %q\n", toolName))
			return m, nil
		}
		m.confirming = true
		m.confirmPrompt = fmt.Sprintf("Allow tool %q? (y/n/a)", toolName)
		m.confirmYes = func() tea.Cmd {
			_ = reqID // TODO: wire RespondPermission via client when permission flow is integrated
			return func() tea.Msg { return repaintMsg{} }
		}
		m.confirmNo = func() tea.Cmd {
			_ = reqID
			return func() tea.Msg { return repaintMsg{} }
		}
	}

	return m, nil
}

// handleConfirmKey processes key input during a confirmation dialog.
func (m *TUIModel) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC, msg.Type == tea.KeyEsc:
		// Cancel = same as "no".
		m.confirming = false
		m.appendOutput(fmt.Sprintf("  %s n\n\n", m.confirmPrompt))
		if m.confirmNo != nil {
			return m, m.confirmNo()
		}
		return m, nil

	case msg.Type == tea.KeyRunes:
		ch := string(msg.Runes)
		if ch == "y" || ch == "Y" {
			m.confirming = false
			m.appendOutput(fmt.Sprintf("  %s y\n\n", m.confirmPrompt))
			if m.confirmYes != nil {
				return m, m.confirmYes()
			}
			return m, nil
		}
		if ch == "n" || ch == "N" {
			m.confirming = false
			m.appendOutput(fmt.Sprintf("  %s n\n\n", m.confirmPrompt))
			if m.confirmNo != nil {
				return m, m.confirmNo()
			}
			return m, nil
		}
		if ch == "a" || ch == "A" {
			// "Always allow" — approve this and all future permission prompts for this session.
			m.confirming = false
			m.permAlwaysAllow = true
			m.appendOutput(fmt.Sprintf("  %s a (always allow for this session)\n\n", m.confirmPrompt))
			if m.confirmYes != nil {
				return m, m.confirmYes()
			}
			return m, nil
		}
		// Ignore other keys.
		return m, nil
	}

	return m, nil
}
