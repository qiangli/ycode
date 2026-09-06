// Package provider normalizes model providers at the harness boundary.
//
// The adapter deliberately exposes no tool registry. Every request carries the
// single model-visible function tool, bashy, and provider output is reduced to
// one canonical event stream before the stage graph observes it.
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	api "github.com/qiangli/ycode/internal/api"
)

const ToolName = "bashy"

type Kind string

const (
	KindMock             Kind = "mock"
	KindAnthropic        Kind = "anthropic"
	KindOpenAICompatible Kind = "openai-compatible"
	KindGemini           Kind = "gemini"
)

// Request is the provider-independent inference input. It intentionally has no
// Tools field: provider choice cannot expand the harness capability surface.
type Request struct {
	Model           string
	System          string
	Messages        []api.Message
	MaxTokens       int
	Stream          bool
	Temperature     *float64
	TopP            *float64
	ReasoningEffort string
}

type EventType string

const (
	EventTextDelta     EventType = "text.delta"
	EventThinkingDelta EventType = "thinking.delta"
	EventToolCall      EventType = "tool.call"
	EventUsage         EventType = "usage"
	EventOutcome       EventType = "outcome"
)

type OutcomeClass string

const (
	OutcomeCompleted     OutcomeClass = "completed"
	OutcomeToolCall      OutcomeClass = "tool_call"
	OutcomeLimit         OutcomeClass = "limit"
	OutcomeCanceled      OutcomeClass = "canceled"
	OutcomeDeadline      OutcomeClass = "deadline"
	OutcomeProviderError OutcomeClass = "provider_error"
	OutcomeProtocolError OutcomeClass = "protocol_error"
)

type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type Usage struct {
	InputTokens        int `json:"input_tokens,omitempty"`
	OutputTokens       int `json:"output_tokens,omitempty"`
	CacheCreationInput int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInput     int `json:"cache_read_input_tokens,omitempty"`
}

type Outcome struct {
	Class      OutcomeClass `json:"class"`
	StopReason string       `json:"stop_reason,omitempty"`
	Error      string       `json:"error,omitempty"`
}

type Event struct {
	Type     EventType `json:"type"`
	Text     string    `json:"text,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	Usage    *Usage    `json:"usage,omitempty"`
	Outcome  *Outcome  `json:"outcome,omitempty"`
}

// Adapter owns provider normalization and capability confinement.
type Adapter struct {
	kind    Kind
	backend api.Provider
}

func New(kind Kind, backend api.Provider) (*Adapter, error) {
	switch kind {
	case KindMock, KindAnthropic, KindOpenAICompatible, KindGemini:
	default:
		return nil, fmt.Errorf("harness provider: unsupported kind %q", kind)
	}
	if backend == nil {
		return nil, errors.New("harness provider: nil backend")
	}
	return &Adapter{kind: kind, backend: backend}, nil
}

func NewAnthropic(backend api.Provider) (*Adapter, error) {
	return New(KindAnthropic, backend)
}

func NewOpenAICompatible(backend api.Provider) (*Adapter, error) {
	return New(KindOpenAICompatible, backend)
}

func NewGemini(backend api.Provider) (*Adapter, error) {
	return New(KindGemini, backend)
}

func (a *Adapter) Kind() Kind { return a.kind }

// BashyTool returns a fresh copy of the sole tool definition.
func BashyTool() api.ToolDefinition {
	return api.ToolDefinition{
		Name:        ToolName,
		Description: "Execute a shell script through the governed Bashy harness.",
		InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"script":{"type":"string"},"timeout_ms":{"type":"integer","minimum":1},"digest":{"type":"string"}},"required":["script"]}`),
	}
}

// Send starts a canonical stream. The last event is always exactly one outcome;
// provider and protocol failures are data, so every stage can branch uniformly.
func (a *Adapter) Send(ctx context.Context, request Request) <-chan Event {
	out := make(chan Event, 16)
	go a.send(ctx, request, out)
	return out
}

func (a *Adapter) send(ctx context.Context, request Request, out chan<- Event) {
	defer close(out)
	if err := validateRequest(request); err != nil {
		emit(ctx, out, outcomeEvent(OutcomeProtocolError, "", err))
		return
	}

	wire := &api.Request{
		Model:           request.Model,
		System:          request.System,
		Messages:        cloneMessages(request.Messages),
		MaxTokens:       request.MaxTokens,
		Stream:          request.Stream,
		Temperature:     request.Temperature,
		TopP:            request.TopP,
		ReasoningEffort: request.ReasoningEffort,
		Tools:           []api.ToolDefinition{BashyTool()},
	}
	events, errs := a.backend.Send(ctx, wire)
	state := normalizer{requestSeed: requestDigest(request), tools: make(map[int]*toolState)}

	for events != nil || errs != nil {
		select {
		case <-ctx.Done():
			class := OutcomeCanceled
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				class = OutcomeDeadline
			}
			emit(context.Background(), out, outcomeEvent(class, state.stopReason, ctx.Err()))
			return
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if err != nil {
				emit(ctx, out, outcomeEvent(OutcomeProviderError, state.stopReason, err))
				return
			}
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			for _, normalized := range state.accept(event) {
				if normalized.Outcome != nil {
					continue
				}
				if !emit(ctx, out, normalized) {
					return
				}
			}
		}
	}

	if len(state.tools) != 0 {
		emit(ctx, out, outcomeEvent(OutcomeProtocolError, state.stopReason, errors.New("provider ended with an incomplete tool call")))
		return
	}
	if state.protocolErr != nil {
		emit(ctx, out, outcomeEvent(OutcomeProtocolError, state.stopReason, state.protocolErr))
		return
	}
	emit(ctx, out, Event{Type: EventOutcome, Outcome: &Outcome{
		Class:      classifyStop(state.stopReason, state.sawTool),
		StopReason: state.stopReason,
	}})
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.Model) == "" {
		return errors.New("model is required")
	}
	if request.MaxTokens <= 0 {
		return errors.New("max tokens must be positive")
	}
	return nil
}

func cloneMessages(messages []api.Message) []api.Message {
	data, _ := json.Marshal(messages)
	var cloned []api.Message
	_ = json.Unmarshal(data, &cloned)
	return cloned
}

type toolState struct {
	call    ToolCall
	partial []byte
}

type normalizer struct {
	requestSeed string
	stopReason  string
	sawTool     bool
	protocolErr error
	tools       map[int]*toolState
	ordinal     int
}

func (n *normalizer) accept(event *api.StreamEvent) []Event {
	if event == nil {
		return nil
	}
	switch event.Type {
	case "message_start":
		if event.Message != nil {
			return usageEvents(event.Message.Usage)
		}
	case "content_block_start":
		block := event.ContentBlock
		if block == nil && len(event.Delta) != 0 {
			block = new(api.ContentBlock)
			if json.Unmarshal(event.Delta, block) != nil {
				return nil
			}
		}
		if block != nil && block.Type == api.ContentTypeToolUse {
			n.tools[event.Index] = &toolState{call: ToolCall{ID: block.ID, Name: block.Name, Input: append(json.RawMessage(nil), block.Input...)}}
		}
	case "content_block_delta":
		var delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		}
		if json.Unmarshal(event.Delta, &delta) != nil {
			return nil
		}
		if delta.Text != "" {
			return []Event{{Type: EventTextDelta, Text: delta.Text}}
		}
		if delta.Thinking != "" {
			return []Event{{Type: EventThinkingDelta, Text: delta.Thinking}}
		}
		if state := n.tools[event.Index]; state != nil && delta.PartialJSON != "" {
			state.partial = append(state.partial, delta.PartialJSON...)
		}
	case "content_block_stop":
		state := n.tools[event.Index]
		if state == nil {
			return nil
		}
		delete(n.tools, event.Index)
		if len(state.partial) != 0 {
			if len(state.call.Input) == 0 || string(state.call.Input) == "{}" {
				state.call.Input = append(json.RawMessage(nil), state.partial...)
			} else {
				state.call.Input = append(state.call.Input, state.partial...)
			}
		}
		if state.call.Name != ToolName {
			n.protocolErr = fmt.Errorf("provider requested forbidden tool %q", state.call.Name)
			return nil
		}
		if !json.Valid(state.call.Input) {
			n.protocolErr = errors.New("provider returned invalid bashy tool input")
			return nil
		}
		if state.call.ID == "" {
			state.call.ID = deterministicCallID(n.requestSeed, n.ordinal, state.call)
		}
		n.ordinal++
		n.sawTool = true
		return []Event{{Type: EventToolCall, ToolCall: &state.call}}
	case "message_delta":
		var result []Event
		if event.Usage != nil {
			result = append(result, usageEvents(*event.Usage)...)
		}
		var delta struct {
			StopReason string `json:"stop_reason"`
		}
		if json.Unmarshal(event.Delta, &delta) == nil && delta.StopReason != "" {
			n.stopReason = delta.StopReason
		}
		return result
	}
	return nil
}

func usageEvents(usage api.Usage) []Event {
	input := usage.InputTokens + usage.PromptTokens
	output := usage.OutputTokens + usage.CompletionTokens
	if input == 0 && output == 0 && usage.CacheCreationInput == 0 && usage.CacheReadInput == 0 {
		return nil
	}
	return []Event{{Type: EventUsage, Usage: &Usage{
		InputTokens: input, OutputTokens: output,
		CacheCreationInput: usage.CacheCreationInput, CacheReadInput: usage.CacheReadInput,
	}}}
}

func requestDigest(request Request) string {
	data, _ := json.Marshal(request)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func deterministicCallID(requestSeed string, ordinal int, call ToolCall) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00", requestSeed, ordinal, call.Name)
	h.Write(call.Input)
	return "call_" + hex.EncodeToString(h.Sum(nil))[:24]
}

func classifyStop(reason string, sawTool bool) OutcomeClass {
	if sawTool || reason == api.StopReasonToolUse {
		return OutcomeToolCall
	}
	if reason == api.StopReasonMaxTokens {
		return OutcomeLimit
	}
	return OutcomeCompleted
}

func outcomeEvent(class OutcomeClass, reason string, err error) Event {
	outcome := &Outcome{Class: class, StopReason: reason}
	if err != nil {
		outcome.Error = err.Error()
	}
	return Event{Type: EventOutcome, Outcome: outcome}
}

func emit(ctx context.Context, out chan<- Event, event Event) bool {
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
