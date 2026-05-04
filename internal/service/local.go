package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/bus"
	"github.com/qiangli/ycode/internal/runtime/config"
	"github.com/qiangli/ycode/internal/runtime/conversation"
	"github.com/qiangli/ycode/internal/runtime/session"
	"github.com/qiangli/ycode/internal/runtime/taskqueue"
)

const maxToolIterations = 25 // kept for reference; IterationBudget is the active mechanism

// LocalService implements Service by wrapping a cli.App instance.
// It bridges the existing App methods into the service interface and
// publishes events to the bus for all transports to consume.
type LocalService struct {
	app AppBackend
	b   bus.Bus

	// OllamaLister queries locally available Ollama models (optional).
	ollamaLister api.OllamaLister

	// Per-session turn cancellation.
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc

	// Per-session write serialization.
	sessionMu sync.Map // map[string]*sync.Mutex

	// Permission response channels keyed by request ID.
	permMu    sync.Mutex
	permChans map[string]chan bool
}

// NewLocalService creates a service backed by a cli.App.
func NewLocalService(app AppBackend, b bus.Bus) *LocalService {
	return &LocalService{
		app:       app,
		b:         b,
		cancels:   make(map[string]context.CancelFunc),
		permChans: make(map[string]chan bool),
	}
}

func (s *LocalService) Bus() bus.Bus {
	return s.b
}

func (s *LocalService) CreateSession(ctx context.Context) (*SessionInfo, error) {
	return s.currentSessionInfo(), nil
}

func (s *LocalService) GetSession(ctx context.Context, id string) (*SessionInfo, error) {
	info := s.currentSessionInfo()
	if info.ID != id {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return info, nil
}

func (s *LocalService) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	return []SessionInfo{*s.currentSessionInfo()}, nil
}

func (s *LocalService) GetMessages(ctx context.Context, sessionID string) ([]json.RawMessage, error) {
	msgs := s.app.SessionMessages()
	var result []json.RawMessage
	for _, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			continue
		}
		result = append(result, data)
	}
	return result, nil
}

// SendMessage runs the full agentic loop for a session. Streaming deltas
// and tool events are published to the bus in real time.
func (s *LocalService) SendMessage(ctx context.Context, sessionID string, input MessageInput) error {
	// Serialize writes per session.
	mu, _ := s.sessionMu.LoadOrStore(sessionID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	ctx, cancel := context.WithCancel(ctx)
	s.cancelMu.Lock()
	s.cancels[sessionID] = cancel
	s.cancelMu.Unlock()

	defer func() {
		s.cancelMu.Lock()
		delete(s.cancels, sessionID)
		s.cancelMu.Unlock()
		cancel()
	}()

	// Detect slash command input and route to command execution.
	if strings.HasPrefix(input.Text, "/") {
		rest := input.Text[1:]
		name, args, _ := strings.Cut(rest, " ")
		if s.app.HasCommand(name) {
			return s.executeCommandFromMessage(ctx, sessionID, name, args)
		}
		// Not a registered command — fall through to agentic loop
		// (e.g. skill slash commands like /claude, /build).
	}

	// Create runtime with event callback.
	rt := s.app.ConversationRuntime()
	rt.SetEventCallback(func(eventType string, data map[string]any) {
		s.b.Publish(bus.Event{
			Type:      bus.EventType(eventType),
			SessionID: sessionID,
			Data:      mustJSON(data),
		})
	})

	// Build conversation history from session + new user message.
	messages := s.app.SessionMessages()
	messages = append(messages, api.Message{
		Role: api.RoleUser,
		Content: []api.ContentBlock{
			{Type: api.ContentTypeText, Text: input.Text},
		},
	})

	// Save user message to session.
	_ = s.app.Session().AddMessage(session.ConversationMessage{
		Role: session.RoleUser,
		Content: []session.ContentBlock{
			{Type: session.ContentTypeText, Text: input.Text},
		},
	})

	// Publish turn start.
	s.b.Publish(bus.Event{
		Type:      bus.EventTurnStart,
		SessionID: sessionID,
		Data:      mustJSON(map[string]string{"text": input.Text}),
	})

	// Agentic loop: send → receive → execute tools → repeat until end_turn.
	loopDetector := conversation.NewEnhancedLoopDetector(conversation.EnhancedLoopDetectorConfig{
		SessionID: sessionID,
	})
	budget := conversation.NewIterationBudget(maxToolIterations)
	for i := 0; budget.Consume(); i++ {
		// Inject grace message on the final turn so the LLM wraps up.
		if budget.IsGrace() {
			messages = append(messages, api.Message{
				Role: api.RoleUser,
				Content: []api.ContentBlock{
					{Type: api.ContentTypeText, Text: budget.GraceMessage()},
				},
			})
		}
		// i is used for error context below.
		turnIdx := s.app.NextTurnIndex()
		result, _, err := rt.InstrumentedTurnWithRecovery(ctx, messages, turnIdx)
		if err != nil {
			s.b.Publish(bus.Event{
				Type:      bus.EventTurnError,
				SessionID: sessionID,
				Data:      mustJSON(map[string]string{"error": err.Error()}),
			})
			return fmt.Errorf("turn %d: %w", i+1, err)
		}

		// Track usage.
		s.app.UsageTracker().Add(
			result.Usage.InputTokens,
			result.Usage.OutputTokens,
			result.Usage.CacheCreationInput,
			result.Usage.CacheReadInput,
		)

		// Publish usage update.
		s.b.Publish(bus.Event{
			Type:      bus.EventUsageUpdate,
			SessionID: sessionID,
			Data: mustJSON(map[string]any{
				"input_tokens":  result.Usage.InputTokens,
				"output_tokens": result.Usage.OutputTokens,
			}),
		})

		// Check for stuck loops (response similarity).
		if result.TextContent != "" {
			loopStatus := loopDetector.RecordResponse(result.TextContent)
			switch loopStatus {
			case conversation.LoopBreak:
				s.b.Publish(bus.Event{
					Type:      bus.EventTurnComplete,
					SessionID: sessionID,
					Data:      mustJSON(map[string]string{"status": "loop_break"}),
				})
				return nil
			case conversation.LoopWarning:
				messages = append(messages, api.Message{
					Role: api.RoleUser,
					Content: []api.ContentBlock{{
						Type: api.ContentTypeText,
						Text: "<system-reminder>You appear to be repeating similar actions. " +
							"Stop searching and try a different approach. For standard commands " +
							"(ssh, ping, curl, etc.) use the bash tool directly.</system-reminder>",
					}},
				})
			}
		}

		// Check for stuck loops (tool call patterns).
		toolLoopWarned := false
		for _, tc := range result.ToolCalls {
			toolStatus := loopDetector.RecordToolCall(tc.Name)
			switch toolStatus {
			case conversation.LoopBreak:
				s.b.Publish(bus.Event{
					Type:      bus.EventTurnComplete,
					SessionID: sessionID,
					Data:      mustJSON(map[string]string{"status": "loop_break"}),
				})
				return nil
			case conversation.LoopWarning:
				toolLoopWarned = true
			}
		}
		if toolLoopWarned {
			messages = append(messages, api.Message{
				Role: api.RoleUser,
				Content: []api.ContentBlock{{
					Type: api.ContentTypeText,
					Text: "<system-reminder>You are calling the same tools repeatedly. " +
						"Stop searching and use the bash tool directly for the operation.</system-reminder>",
				}},
			})
		}

		// Save assistant message to session.
		if result.TextContent != "" {
			_ = s.app.Session().AddMessage(session.ConversationMessage{
				Role: session.RoleAssistant,
				Content: []session.ContentBlock{
					{Type: session.ContentTypeText, Text: result.TextContent},
				},
			})
		}

		// If no tool calls, we're done.
		if len(result.ToolCalls) == 0 {
			s.b.Publish(bus.Event{
				Type:      bus.EventTurnComplete,
				SessionID: sessionID,
				Data:      mustJSON(map[string]string{"status": "complete", "text": result.TextContent}),
			})
			return nil
		}

		// Build assistant message with tool_use blocks.
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
		messages = append(messages, api.Message{
			Role:    api.RoleAssistant,
			Content: assistantBlocks,
		})

		// Execute tools with progress events via the taskqueue channel.
		progressCh := make(chan taskqueue.TaskEvent, 64)
		go func() {
			for ev := range progressCh {
				status := "queued"
				switch ev.Status {
				case taskqueue.StatusRunning:
					status = "running"
				case taskqueue.StatusCompleted:
					status = "completed"
				case taskqueue.StatusFailed:
					status = "failed"
				}
				s.b.Publish(bus.Event{
					Type:      bus.EventToolProgress,
					SessionID: sessionID,
					Data: mustJSON(map[string]any{
						"tool":   ev.Name,
						"status": status,
						"index":  ev.Index,
						"total":  ev.Total,
					}),
				})
			}
		}()

		toolResults := rt.ExecuteTools(ctx, result.ToolCalls, progressCh)
		close(progressCh)

		// Publish tool results.
		for _, block := range toolResults {
			status := "completed"
			if block.IsError {
				status = "failed"
			}
			s.b.Publish(bus.Event{
				Type:      bus.EventToolResult,
				SessionID: sessionID,
				Data: mustJSON(map[string]any{
					"tool_use_id": block.ToolUseID,
					"status":      status,
					"is_error":    block.IsError,
				}),
			})
		}

		// Append tool results to conversation.
		messages = append(messages, api.Message{
			Role:    api.RoleUser,
			Content: toolResults,
		})
	}

	s.b.Publish(bus.Event{
		Type:      bus.EventTurnComplete,
		SessionID: sessionID,
		Data:      mustJSON(map[string]string{"status": "max_iterations"}),
	})
	return nil
}

// executeCommandFromMessage runs a slash command with streaming progress via bus events.
func (s *LocalService) executeCommandFromMessage(ctx context.Context, sessionID, name, args string) error {
	// Wire progress/delta callbacks to publish bus events.
	s.app.SetProgressFunc(func(message string) {
		s.b.Publish(bus.Event{
			Type:      bus.EventCommandProgress,
			SessionID: sessionID,
			Data:      mustJSON(map[string]string{"message": message}),
		})
	})
	s.app.SetDeltaFunc(func(text string) {
		s.b.Publish(bus.Event{
			Type:      bus.EventCommandDelta,
			SessionID: sessionID,
			Data:      mustJSON(map[string]string{"text": text}),
		})
	})
	defer func() {
		s.app.SetProgressFunc(nil)
		s.app.SetDeltaFunc(nil)
	}()

	result, err := s.app.ExecuteCommand(ctx, name, args)
	if err != nil {
		s.b.Publish(bus.Event{
			Type:      bus.EventCommandError,
			SessionID: sessionID,
			Data:      mustJSON(map[string]string{"error": err.Error()}),
		})
		return err
	}

	s.b.Publish(bus.Event{
		Type:      bus.EventCommandComplete,
		SessionID: sessionID,
		Data:      mustJSON(map[string]string{"result": result}),
	})
	return nil
}

func (s *LocalService) CancelTurn(ctx context.Context, sessionID string) error {
	s.cancelMu.Lock()
	cancel, ok := s.cancels[sessionID]
	s.cancelMu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

func (s *LocalService) RespondPermission(ctx context.Context, requestID string, allowed bool) error {
	s.permMu.Lock()
	ch, ok := s.permChans[requestID]
	s.permMu.Unlock()
	if !ok {
		return fmt.Errorf("no pending permission request %q", requestID)
	}
	ch <- allowed
	return nil
}

func (s *LocalService) GetConfig(ctx context.Context) (*config.Config, error) {
	return s.app.Config(), nil
}

func (s *LocalService) SwitchModel(ctx context.Context, model string) error {
	_, err := s.app.SwitchModel(model)
	return err
}

func (s *LocalService) GetStatus(ctx context.Context) (*StatusInfo, error) {
	return &StatusInfo{
		Model:        s.app.Model(),
		ProviderKind: s.app.ProviderKind(),
		SessionID:    s.app.SessionID(),
		PlanMode:     s.app.InPlanMode(),
		Version:      s.app.Version(),
	}, nil
}

func (s *LocalService) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	aliases := s.app.Config().Aliases
	return api.DiscoverModels(ctx, aliases, s.ollamaLister), nil
}

// SetOllamaLister sets the callback for discovering local Ollama models.
func (s *LocalService) SetOllamaLister(lister api.OllamaLister) {
	s.ollamaLister = lister
}

func (s *LocalService) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	return s.app.ExecuteCommand(ctx, name, args)
}

func (s *LocalService) currentSessionInfo() *SessionInfo {
	return &SessionInfo{
		ID:           s.app.SessionID(),
		MessageCount: s.app.MessageCount(),
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
