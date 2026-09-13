// Package ioctx implements the policy-free input, context, prompt and output
// mechanisms used by the YAML harness stage graph.
package ioctx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/spec"
)

const SchemaVersion = "ycode.ioctx/v1"

type TokenCounter interface {
	Count(text string) (int, error)
}

type DeliveryRequest struct {
	EventID     string
	SinkRef     string
	FrontendRef string
	PayloadRef  string
	Content     []byte
}

type Delivery interface {
	Deliver(context.Context, DeliveryRequest) error
}

type Redactor interface {
	Redact(policy string, content []byte) ([]byte, error)
}

type DeadLetterRequest struct {
	ControlPath string
	Delivery    DeliveryRequest
	Cause       string
}

type DeadLetter interface {
	Store(context.Context, DeadLetterRequest) error
}

type Config struct {
	Document     *spec.Document
	Events       *event.Store
	Payloads     *event.PayloadStore
	TokenCounter TokenCounter
	Delivery     Delivery
	Redactor     Redactor
	DeadLetter   DeadLetter
}

type Engine struct {
	sources    map[string]spec.Source
	contexts   map[string]spec.Context
	frontends  map[string]spec.Frontend
	triggers   map[string]spec.Trigger
	sinks      map[string]spec.Sink
	events     *event.Store
	payloads   *event.PayloadStore
	tokens     TokenCounter
	delivery   Delivery
	redactor   Redactor
	deadLetter DeadLetter
}

func New(config Config) (*Engine, error) {
	if config.Document == nil || config.Events == nil || config.Payloads == nil || config.TokenCounter == nil {
		return nil, errors.New("ioctx requires compiled document, event store, payload store and token counter")
	}
	return &Engine{
		sources: cloneSources(config.Document.Spec.Sources), contexts: cloneContexts(config.Document.Spec.Contexts),
		frontends: cloneFrontends(config.Document.Spec.Frontends), triggers: cloneTriggers(config.Document.Spec.Triggers),
		sinks: cloneSinks(config.Document.Spec.Sinks), events: config.Events, payloads: config.Payloads,
		tokens: config.TokenCounter, delivery: config.Delivery, redactor: config.Redactor, deadLetter: config.DeadLetter,
	}, nil
}

type Meta struct {
	SessionID    string
	RunID        string
	StageID      string
	ConfigDigest string
}

func (m Meta) validate() error {
	if m.SessionID == "" || m.RunID == "" || m.StageID == "" || m.ConfigDigest == "" {
		return errors.New("ioctx stage requires session, run, stage and config digest")
	}
	return nil
}

type AdmissionRequest struct {
	TriggerRef     string
	FrontendRef    string
	Principal      string
	IdempotencyKey string
	Body           []byte
}

type CanonicalInput struct {
	SchemaVersion  string          `json:"schema_version"`
	TriggerRef     string          `json:"trigger_ref"`
	FrontendRef    string          `json:"frontend_ref"`
	Principal      string          `json:"principal"`
	IdempotencyKey string          `json:"idempotency_key"`
	Data           json.RawMessage `json:"data"`
	PayloadRef     string          `json:"payload_ref"`
}

// Admit validates one frontend request and snapshots its canonical JSON bytes.
func (e *Engine) Admit(_ context.Context, meta Meta, request AdmissionRequest) (CanonicalInput, error) {
	if err := meta.validate(); err != nil {
		return CanonicalInput{}, err
	}
	trigger, ok := e.triggers[request.TriggerRef]
	if !ok || trigger.Kind != "frontend" || trigger.InputMapping != "canonical-v1" {
		return CanonicalInput{}, fmt.Errorf("input admission: invalid compiled trigger %q", request.TriggerRef)
	}
	if !contains(trigger.FrontendRefs, request.FrontendRef) {
		return CanonicalInput{}, fmt.Errorf("input admission: frontend %q is not routed by trigger %q", request.FrontendRef, request.TriggerRef)
	}
	frontend, ok := e.frontends[request.FrontendRef]
	if !ok || frontend.Limits.MaxInputBytes <= 0 {
		return CanonicalInput{}, fmt.Errorf("input admission: invalid compiled frontend %q", request.FrontendRef)
	}
	if request.Principal == "" || len(request.Body) == 0 || len(request.Body) > frontend.Limits.MaxInputBytes {
		return CanonicalInput{}, errors.New("input admission: principal and bounded non-empty body are required")
	}
	if trigger.Idempotency == "required" && request.IdempotencyKey == "" {
		return CanonicalInput{}, errors.New("input admission: idempotency key is required")
	}
	if trigger.Idempotency != "required" {
		return CanonicalInput{}, fmt.Errorf("input admission: unsupported idempotency mode %q", trigger.Idempotency)
	}
	canonical, err := canonicalJSON(request.Body)
	if err != nil {
		return CanonicalInput{}, fmt.Errorf("input admission: %w", err)
	}
	ref, err := e.payloads.Put(canonical)
	if err != nil {
		return CanonicalInput{}, err
	}
	result := CanonicalInput{SchemaVersion: SchemaVersion, TriggerRef: request.TriggerRef, FrontendRef: request.FrontendRef, Principal: request.Principal, IdempotencyKey: request.IdempotencyKey, Data: canonical, PayloadRef: ref}
	_, err = e.append(meta, "input.admitted", map[string]any{
		"schema_version": SchemaVersion, "trigger_ref": request.TriggerRef, "frontend_ref": request.FrontendRef,
		"principal": request.Principal, "idempotency_key": request.IdempotencyKey, "payload_ref": ref, "bytes": len(canonical),
	})
	return result, err
}

type Fragment struct {
	ID         string `json:"id"`
	SourceRef  string `json:"source_ref"`
	Role       string `json:"role"`
	CacheScope string `json:"cache_scope"`
	BreakAfter bool   `json:"break_after"`
	Content    string `json:"content"`
	PayloadRef string `json:"payload_ref"`
	Bytes      int    `json:"bytes"`
	Tokens     int    `json:"tokens"`
}

type ContextSnapshot struct {
	SchemaVersion string     `json:"schema_version"`
	ContextRef    string     `json:"context_ref"`
	Fragments     []Fragment `json:"fragments"`
	Tokens        int        `json:"tokens"`
}

// LoadContext snapshots sources in the exact fragment-list order compiled from YAML.
func (e *Engine) LoadContext(_ context.Context, meta Meta, contextRef string) (ContextSnapshot, error) {
	if err := meta.validate(); err != nil {
		return ContextSnapshot{}, err
	}
	configured, ok := e.contexts[contextRef]
	if !ok || configured.Budget.MaxTokens <= 0 || configured.Budget.Overflow != "fail" {
		return ContextSnapshot{}, fmt.Errorf("context load: invalid compiled context %q", contextRef)
	}
	result := ContextSnapshot{SchemaVersion: SchemaVersion, ContextRef: contextRef}
	seen := make(map[string]struct{}, len(configured.Fragments))
	for _, declaration := range configured.Fragments {
		if declaration.ID == "" || declaration.SourceRef == "" || declaration.Role == "" {
			return ContextSnapshot{}, errors.New("context load: incomplete fragment declaration")
		}
		if _, duplicate := seen[declaration.ID]; duplicate {
			return ContextSnapshot{}, fmt.Errorf("context load: duplicate fragment %q", declaration.ID)
		}
		seen[declaration.ID] = struct{}{}
		source, exists := e.sources[declaration.SourceRef]
		if !exists || source.Limits.MaxBytes <= 0 || len([]byte(source.Resolved)) > source.Limits.MaxBytes {
			return ContextSnapshot{}, fmt.Errorf("context load: invalid compiled source %q", declaration.SourceRef)
		}
		count, err := e.tokens.Count(source.Resolved)
		if err != nil {
			return ContextSnapshot{}, fmt.Errorf("context load: count source %q: %w", declaration.SourceRef, err)
		}
		if count < 0 {
			return ContextSnapshot{}, fmt.Errorf("context load: negative token count for source %q", declaration.SourceRef)
		}
		if result.Tokens+count > configured.Budget.MaxTokens {
			return ContextSnapshot{}, fmt.Errorf("context load: token budget exceeded at fragment %q", declaration.ID)
		}
		ref, err := e.payloads.Put([]byte(source.Resolved))
		if err != nil {
			return ContextSnapshot{}, err
		}
		result.Tokens += count
		result.Fragments = append(result.Fragments, Fragment{ID: declaration.ID, SourceRef: declaration.SourceRef, Role: declaration.Role, CacheScope: declaration.Cache.Scope, BreakAfter: declaration.Cache.BreakAfter, Content: source.Resolved, PayloadRef: ref, Bytes: len([]byte(source.Resolved)), Tokens: count})
	}
	refs := make([]map[string]any, 0, len(result.Fragments))
	for position, fragment := range result.Fragments {
		refs = append(refs, map[string]any{"position": position, "id": fragment.ID, "source_ref": fragment.SourceRef, "role": fragment.Role, "cache_scope": fragment.CacheScope, "break_after": fragment.BreakAfter, "payload_ref": fragment.PayloadRef, "bytes": fragment.Bytes, "tokens": fragment.Tokens})
	}
	_, err := e.append(meta, "context.loaded", map[string]any{"schema_version": SchemaVersion, "context_ref": contextRef, "tokens": result.Tokens, "fragments": refs})
	return result, err
}

type PortName string

const (
	PortContext   PortName = "context"
	PortKnowledge PortName = "knowledge"
	PortInput     PortName = "input"
)

// PromptMessage optionally carries kb provenance: Ring names the store a
// knowledge block came from, Form its record shape, and Ref its stable
// reference (kb:<slug> | graph:<id> | code:<id>). All three are empty for
// context and input messages.
type PromptMessage struct {
	Port       PortName `json:"port"`
	Role       string   `json:"role"`
	Content    string   `json:"content"`
	PayloadRef string   `json:"payload_ref"`
	Ring       string   `json:"ring,omitempty"`
	Form       string   `json:"form,omitempty"`
	Ref        string   `json:"ref,omitempty"`
}

type PromptRequest struct {
	// Order must come from the compiled stage configuration. No default order
	// exists because changing it changes provider-visible semantics.
	Order     []PortName
	Context   ContextSnapshot
	Knowledge []PromptMessage
	Input     CanonicalInput
}

type Prompt struct {
	SchemaVersion string          `json:"schema_version"`
	Messages      []PromptMessage `json:"messages"`
	PayloadRef    string          `json:"payload_ref"`
}

type PromptPorts struct {
	Context   ContextSnapshot
	Knowledge []PromptMessage
	Input     CanonicalInput
}

// AssembleStage binds the mechanism to a compiled prompt.assemble node. The
// provider-visible port order must be an explicit run.with.order YAML list.
func (e *Engine) AssembleStage(ctx context.Context, meta Meta, run spec.Run, ports PromptPorts) (Prompt, error) {
	if run.Stage != "prompt.assemble" {
		return Prompt{}, fmt.Errorf("prompt assembly: got compiled stage %q", run.Stage)
	}
	for _, name := range []string{"context", "knowledge", "input"} {
		if run.In[name] == "" {
			return Prompt{}, fmt.Errorf("prompt assembly: missing typed input port %q", name)
		}
	}
	order, err := portOrder(run.With["order"])
	if err != nil {
		return Prompt{}, err
	}
	return e.Assemble(ctx, meta, PromptRequest{Order: order, Context: ports.Context, Knowledge: ports.Knowledge, Input: ports.Input})
}

func (e *Engine) Assemble(_ context.Context, meta Meta, request PromptRequest) (Prompt, error) {
	if err := meta.validate(); err != nil {
		return Prompt{}, err
	}
	if len(request.Order) == 0 {
		return Prompt{}, errors.New("prompt assembly: explicit typed port order is required")
	}
	result := Prompt{SchemaVersion: SchemaVersion}
	seen := make(map[PortName]struct{}, len(request.Order))
	for _, port := range request.Order {
		if _, duplicate := seen[port]; duplicate {
			return Prompt{}, fmt.Errorf("prompt assembly: duplicate port %q", port)
		}
		seen[port] = struct{}{}
		switch port {
		case PortContext:
			for _, fragment := range request.Context.Fragments {
				result.Messages = append(result.Messages, PromptMessage{Port: port, Role: fragment.Role, Content: fragment.Content, PayloadRef: fragment.PayloadRef})
			}
		case PortKnowledge:
			for _, message := range request.Knowledge {
				message.Port = port
				result.Messages = append(result.Messages, message)
			}
		case PortInput:
			result.Messages = append(result.Messages, PromptMessage{Port: port, Role: "user", Content: string(request.Input.Data), PayloadRef: request.Input.PayloadRef})
		default:
			return Prompt{}, fmt.Errorf("prompt assembly: unknown typed port %q", port)
		}
	}
	encoded, err := json.Marshal(result.Messages)
	if err != nil {
		return Prompt{}, err
	}
	result.PayloadRef, err = e.payloads.Put(encoded)
	if err != nil {
		return Prompt{}, err
	}
	refs := make([]map[string]any, 0, len(result.Messages))
	for position, message := range result.Messages {
		ref := map[string]any{"position": position, "port": message.Port, "role": message.Role, "payload_ref": message.PayloadRef}
		if message.Ring != "" || message.Form != "" || message.Ref != "" {
			ref["ring"], ref["form"], ref["ref"] = message.Ring, message.Form, message.Ref
		}
		refs = append(refs, ref)
	}
	_, err = e.append(meta, "prompt.assembled", map[string]any{"schema_version": SchemaVersion, "payload_ref": result.PayloadRef, "messages": refs})
	return result, err
}

func (e *Engine) append(meta Meta, kind string, data any) (event.Event, error) {
	return e.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: kind, ConfigDigest: meta.ConfigDigest, Data: data})
}

func canonicalJSON(input []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON values")
	}
	return json.Marshal(value)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func portOrder(value any) ([]PortName, error) {
	var raw []string
	switch typed := value.(type) {
	case []string:
		raw = append(raw, typed...)
	case []any:
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("prompt assembly: run.with.order must contain strings")
			}
			raw = append(raw, text)
		}
	default:
		return nil, errors.New("prompt assembly: run.with.order must be an explicit list")
	}
	order := make([]PortName, len(raw))
	for i := range raw {
		order[i] = PortName(raw[i])
	}
	return order, nil
}

func cloneSources(in map[string]spec.Source) map[string]spec.Source {
	out := make(map[string]spec.Source, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
func cloneContexts(in map[string]spec.Context) map[string]spec.Context {
	out := make(map[string]spec.Context, len(in))
	for key, value := range in {
		value.Fragments = append([]spec.ContextFragment(nil), value.Fragments...)
		out[key] = value
	}
	return out
}
func cloneFrontends(in map[string]spec.Frontend) map[string]spec.Frontend {
	out := make(map[string]spec.Frontend, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
func cloneTriggers(in map[string]spec.Trigger) map[string]spec.Trigger {
	out := make(map[string]spec.Trigger, len(in))
	for key, value := range in {
		value.FrontendRefs = append([]string(nil), value.FrontendRefs...)
		out[key] = value
	}
	return out
}
func cloneSinks(in map[string]spec.Sink) map[string]spec.Sink {
	out := make(map[string]spec.Sink, len(in))
	for key, value := range in {
		value.FrontendRefs = append([]string(nil), value.FrontendRefs...)
		value.DestinationAllowlist = append([]string(nil), value.DestinationAllowlist...)
		out[key] = value
	}
	return out
}
