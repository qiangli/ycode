package ycode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/qiangli/ycode/internal/api"
	harnessbashy "github.com/qiangli/ycode/internal/harness/bashy"
	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/observe"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	"github.com/qiangli/ycode/internal/harness/turn"
	"go.opentelemetry.io/otel/trace"
)

// Provider and its wire types are aliases so embedders can supply a provider
// without importing an internal package.
type Provider = api.Provider
type ProviderRequest = api.Request
type ProviderStreamEvent = api.StreamEvent
type ProviderKind = api.ProviderKind
type BashyCall = hitl.Call
type Event = event.Event

const (
	ProviderAnthropic ProviderKind = api.ProviderAnthropic
	ProviderOpenAI    ProviderKind = api.ProviderOpenAI
	ProviderGemini    ProviderKind = api.ProviderGemini
	ProviderLocal     ProviderKind = api.ProviderLocal
)

type LoadOption func(*loadOptions) error
type loadOptions struct {
	providers map[string]api.Provider
	tracer    trace.Tracer
	decorate  func(string, Provider) Provider
}

// WithHarnessProviderDecorator instruments the transport boundary without
// changing compiled routes, models, retries, or authorization policy.
func WithHarnessProviderDecorator(decorate func(string, Provider) Provider) LoadOption {
	return func(options *loadOptions) error { options.decorate = decorate; return nil }
}

// WithHarnessProvider overrides one provider resource at the transport seam;
// routing, models, retry and tool policy remain compiled YAML controls.
func WithHarnessProvider(ref string, backend Provider) LoadOption {
	return func(options *loadOptions) error {
		if ref == "" || backend == nil {
			return errors.New("harness provider override requires ref and backend")
		}
		if options.providers == nil {
			options.providers = make(map[string]api.Provider)
		}
		options.providers[ref] = backend
		return nil
	}
}

// WithHarnessTracer enables the compiled observability policy on the neutral
// pipeline. A disabled YAML observability resource still emits no spans.
func WithHarnessTracer(tracer trace.Tracer) LoadOption {
	return func(options *loadOptions) error { options.tracer = tracer; return nil }
}

type Harness struct {
	doc       *spec.Document
	events    *event.Store
	payloads  *event.PayloadStore
	io        *ioctx.Engine
	turn      *turn.Runtime
	eventPath string
	control   string
	queue     *sessionQueue

	mu     sync.Mutex
	active map[string]*activeRun
}

type activeRun struct {
	aside  bool
	pause  pauseGate
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

type RunRequest struct {
	Aside bool
	// Plan selects the agent's declared tool-free planning behavior.
	Plan           bool
	SessionID      string
	RunID          string
	TriggerRef     string
	FrontendRef    string
	AgentRef       string
	Principal      string
	IdempotencyKey string
	Body           []byte
	HumanAvailable bool
}

type ResumeRequest struct {
	SessionID       string
	RunID           string
	DecisionID      string
	ExpectedVersion uint64
	ReviewDigest    string
	ReportDigest    string
	Action          string
	Actor           string
	EditedCall      *BashyCall
}

// ForkRequest creates a durable child session at an exact completed turn
// boundary. Fork records state and emits a canonical event; it never starts a
// provider/tool loop.
type ForkRequest struct {
	ParentSessionID string
	SessionID       string
	RunID           string
	AtSequence      uint64
}

type turnBoundary struct {
	ModelRef     string       `json:"model_ref,omitempty"`
	ConfigDigest string       `json:"config_digest"`
	SessionID    string       `json:"session_id"`
	RunID        string       `json:"run_id"`
	MessagesRef  string       `json:"messages_ref,omitempty"`
	Output       ioctx.Output `json:"output"`
}

// Validate strictly compiles an agent.yaml without opening runtime state.
func Validate(path string) error { _, err := spec.Load(path); return err }

// ValidateSource compiles an in-memory artifact. source names its original
// location so imports and declared roots retain their normal meaning.
func ValidateSource(source string, data []byte) error {
	_, err := spec.Compile(source, data)
	return err
}

// LoadSource uses the same compiler and runtime as Load, without materializing
// a temporary configuration or changing source-relative policy.
func LoadSource(source string, data []byte, option ...LoadOption) (*Harness, error) {
	prepared, err := PrepareSource(source, data)
	if err != nil {
		return nil, err
	}
	return prepared.Load(option...)
}

// PreparedHarness freezes the strictly compiled document, including imports
// and resolved sources, until the caller is authorized to open runtime state.
type PreparedHarness struct{ doc *spec.Document }

func PrepareSource(source string, data []byte) (*PreparedHarness, error) {
	doc, err := spec.Compile(source, data)
	if err != nil {
		return nil, err
	}
	return &PreparedHarness{doc: doc}, nil
}

func (p *PreparedHarness) Digest() string { return p.doc.ConfigDigest }
func (p *PreparedHarness) Load(options ...LoadOption) (*Harness, error) {
	if p == nil || p.doc == nil {
		return nil, errors.New("harness source is not prepared")
	}
	return loadDocument(p.doc, options...)
}

// Load compiles agent.yaml and opens only the mechanisms declared by it.
func Load(path string, option ...LoadOption) (*Harness, error) {
	doc, err := spec.Load(path)
	if err != nil {
		return nil, err
	}
	return loadDocument(doc, option...)
}

func loadDocument(doc *spec.Document, option ...LoadOption) (*Harness, error) {
	options := loadOptions{}
	for _, apply := range option {
		if apply != nil {
			if err := apply(&options); err != nil {
				return nil, err
			}
		}
	}
	control, err := harnessControlRoot(doc)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(control, 0o700); err != nil {
		return nil, err
	}
	eventPath := filepath.Join(control, "sessions", "events.jsonl")
	events, err := event.Open(eventPath)
	if err != nil {
		return nil, err
	}
	payloads, err := event.OpenPayloadStore(filepath.Join(control, "payloads"))
	if err != nil {
		return nil, err
	}
	// Memory recall and persist are compiled bashy.run nodes over `bashy kb`;
	// the harness opens no memory store of its own.
	memoryEngine, err := memoryStage.New(memoryStage.Config{Document: doc, Events: events, Payloads: payloads, Tokens: harnessTokens{}})
	if err != nil {
		return nil, err
	}
	delivery := &embeddedDelivery{}
	redactions := []string{control}
	for _, configured := range doc.Spec.Providers {
		if secret := os.Getenv(configured.Credentials.APIKey.SecretRef.Name); secret != "" {
			redactions = append(redactions, secret)
		}
	}
	ioEngine, err := ioctx.New(ioctx.Config{Document: doc, Events: events, Payloads: payloads, TokenCounter: harnessTokens{}, Delivery: delivery, Redactor: embeddedRedactor{values: redactions}, DeadLetter: fileDeadLetter{root: control}})
	if err != nil {
		return nil, err
	}
	key, err := authorizationKey(filepath.Join(control, "authorization.key"))
	if err != nil {
		return nil, err
	}
	workspace := doc.Spec.Runtime.Workspace
	if !filepath.IsAbs(workspace) {
		workspace = filepath.Join(doc.BaseDir, workspace)
	}
	bashyConfig := doc.Spec.Bashy.Execution
	// The resource key and provider contract name the sole public tool; ToolName
	// is a compiled-view field rather than an additional user setting.
	bashyConfig.ToolName = provider.ToolName
	bashyExecutor, err := harnessbashy.NewExecutor(bashyConfig, workspace, harnessbashy.RuntimeOptions{ControlRoot: filepath.Join(control, "bashy"), AuthorizationKey: key}, events)
	if err != nil {
		return nil, err
	}
	hitlController, err := hitl.New(hitl.Config{Document: doc, Events: events, Payloads: payloads, CheckpointPath: filepath.Join(control, "hitl.json"), Preflighter: bashyExecutor})
	if err != nil {
		return nil, err
	}
	providers, err := harnessProviders(doc, options.providers, options.decorate)
	if err != nil {
		return nil, err
	}
	queue := newSessionQueue(doc, events, payloads, eventPath)
	runtime, err := turn.New(turn.Config{Document: doc, Events: events, Payloads: payloads, IO: ioEngine, Memory: memoryEngine, HITL: hitlController, Bashy: bashyExecutor, Providers: providers, Queue: queue, Observer: observe.New(doc.Spec.Observability, options.tracer), EventPath: eventPath, Tokens: harnessTokens{}})
	if err != nil {
		return nil, err
	}
	memoryEngine.BindSummary(routeSummarizerFor(runtime))
	return &Harness{doc: doc, events: events, payloads: payloads, io: ioEngine, turn: runtime, eventPath: eventPath, control: control, queue: queue, active: make(map[string]*activeRun)}, nil
}

func (h *Harness) Validate() error {
	if h == nil || h.doc == nil || h.doc.ConfigDigest == "" {
		return errors.New("harness is not loaded")
	}
	return nil
}

func (h *Harness) Run(ctx context.Context, request RunRequest) (<-chan Event, error) {
	return h.run(ctx, request, nil)
}

func (h *Harness) run(ctx context.Context, request RunRequest, prepare func() error) (<-chan Event, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if request.SessionID == "" || request.RunID == "" || request.TriggerRef == "" || request.FrontendRef == "" || request.Principal == "" || request.IdempotencyKey == "" {
		return nil, errors.New("harness run requires session, run, trigger, frontend, principal and idempotency key")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var releaseLock = func() {}
	var err error
	if !request.Aside {
		lock, err := h.lockSession(request.SessionID)
		if err != nil {
			return nil, err
		}
		releaseLock = func() { _ = lock.Release() }
	}
	started := false
	defer func() {
		if !started {
			releaseLock()
		}
	}()
	if h.busySession(request.SessionID) && !request.Aside {
		return nil, errors.New("session has an active turn")
	}
	if request.AgentRef == "" {
		request.AgentRef = h.doc.Spec.Triggers[request.TriggerRef].Route.AgentRef
	}
	if request.AgentRef != h.doc.Spec.Triggers[request.TriggerRef].Route.AgentRef {
		return nil, errors.New("agent does not match the authorized trigger")
	}
	if err := h.sessionConfig(request.SessionID); err != nil {
		return nil, err
	}
	modelRef, err := h.sessionModel(request.SessionID)
	if err != nil {
		return nil, err
	}
	mode, err := h.SessionMode(request.SessionID)
	if err != nil {
		return nil, err
	}
	request.Plan = request.Plan || mode == "plan"
	if request.Plan || request.Aside {
		controls := h.doc.Spec.Agents[request.AgentRef].SessionControls
		if controls.PipelineRef == "" || controls.PlanPrompt == "" || (request.Aside && controls.BtwPrompt == "") {
			return nil, errors.New("session control graph is not declared")
		}
	}
	if modelRef != "" {
		if err := h.validateSessionModel(request.AgentRef, modelRef); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start, err := nextSequence(h.eventPath)
	if err != nil {
		return nil, err
	}
	input, err := h.io.Admit(ctx, ioctx.Meta{SessionID: request.SessionID, RunID: request.RunID, StageID: "input.admit", ConfigDigest: h.doc.ConfigDigest}, ioctx.AdmissionRequest{TriggerRef: request.TriggerRef, FrontendRef: request.FrontendRef, Principal: request.Principal, IdempotencyKey: request.IdempotencyKey, Body: request.Body})
	if err != nil {
		return nil, err
	}
	if request.AgentRef == "" {
		request.AgentRef = h.doc.Spec.Triggers[request.TriggerRef].Route.AgentRef
	}
	// Admission must succeed before Retry replaces history. Keep the session
	// lock throughout validation, preparation and the entire replacement turn.
	if prepare != nil {
		if err := prepare(); err != nil {
			return nil, err
		}
	}
	key := runKey(request.SessionID, request.RunID)
	runCtx, cancel := context.WithCancel(ctx)
	active := &activeRun{done: make(chan struct{}), cancel: cancel, aside: request.Aside}
	if _, exists := h.active[key]; exists {
		cancel()
		return nil, errors.New("harness run is already active")
	}
	requestRef, err := h.payloads.Put(mustJSON(request))
	if err != nil {
		cancel()
		return nil, err
	}
	requestType := "session.requested"
	if request.Aside {
		requestType = "session.aside-requested"
	}
	if _, err = h.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, StageID: "session.request", Type: requestType, ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"request_ref": requestRef}}); err != nil {
		cancel()
		return nil, err
	}
	controlCursor, err := h.events.Cursor()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("event log cursor: %w", err)
	}
	h.active[key] = active
	started = true
	go func() {
		defer cancel()
		controlCtx, stopControls := context.WithCancel(runCtx)
		controlsDone := make(chan struct{})
		go func() {
			defer close(controlsDone)
			h.monitorControls(controlCtx, request.SessionID, request.RunID, controlCursor, active)
		}()
		var output ioctx.Output
		output, active.err = h.turn.Run(runCtx, turn.Request{SessionID: request.SessionID, RunID: request.RunID, AgentRef: request.AgentRef, OriginFrontend: request.FrontendRef, HumanAvailable: request.HumanAvailable, Input: input, ModelRef: modelRef, Plan: request.Plan, Aside: request.Aside, Boundary: active.pause.boundary, EnterStage: active.pause.enter})
		if active.err == nil {
			active.err = active.pause.finish(runCtx)
		}
		stopControls()
		<-controlsDone
		_, finishErr := h.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, StageID: "session.control", Type: "session.run-finished", ConfigDigest: h.doc.ConfigDigest})
		if active.err == nil {
			active.err = finishErr
		}
		if active.err != nil {
			_, _ = h.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, StageID: "turn", Type: "turn.failed", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"error": active.err.Error()}})
		} else {
			_, active.err = h.events.SaveCheckpoint(h.boundaryPath(request.SessionID, request.RunID), request.SessionID, request.RunID, turnBoundary{ConfigDigest: h.doc.ConfigDigest, SessionID: request.SessionID, RunID: request.RunID, MessagesRef: output.MessagesRef, Output: output, ModelRef: modelRef})
			if active.err != nil {
				_, _ = h.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, StageID: "turn", Type: "turn.failed", ConfigDigest: h.doc.ConfigDigest, Data: map[string]any{"error": active.err.Error()}})
			}
		}
		h.mu.Lock()
		releaseLock()
		delete(h.active, key)
		close(active.done)
		h.mu.Unlock()
	}()
	return h.stream(ctx, request.SessionID, request.RunID, start, active), nil
}

// Fork durably derives a child session from a completed parent turn boundary.
// It deliberately does not call Runtime.Run: frontends can negotiate or
// persist lifecycle operations without accidentally creating another turn.
func (h *Harness) Fork(ctx context.Context, request ForkRequest) (<-chan Event, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	if request.ParentSessionID == "" || request.SessionID == "" || request.RunID == "" || request.AtSequence == 0 {
		return nil, errors.New("harness fork requires parent session, child session, run and sequence")
	}
	if request.ParentSessionID == request.SessionID {
		return nil, errors.New("harness fork child must differ from parent")
	}
	events, err := event.Replay(h.eventPath)
	if err != nil {
		return nil, err
	}
	var boundaryEvent event.Event
	for _, item := range events {
		if item.Sequence == request.AtSequence && item.SessionID == request.ParentSessionID {
			boundaryEvent = item
			break
		}
	}
	if boundaryEvent.Sequence == 0 {
		return nil, errors.New("harness fork boundary is not in the parent session")
	}
	raw, err := os.ReadFile(h.boundaryPath(request.ParentSessionID, boundaryEvent.RunID))
	if err != nil {
		return nil, fmt.Errorf("harness fork completed boundary: %w", err)
	}
	var parent event.Checkpoint
	if err := json.Unmarshal(raw, &parent); err != nil {
		return nil, fmt.Errorf("harness fork decode boundary: %w", err)
	}
	if parent.SessionID != request.ParentSessionID || parent.RunID != boundaryEvent.RunID || parent.Sequence < request.AtSequence {
		return nil, errors.New("harness fork boundary checkpoint does not cover requested sequence")
	}
	var state turnBoundary
	if err := json.Unmarshal(parent.State, &state); err != nil || state.ConfigDigest != h.doc.ConfigDigest || state.SessionID != parent.SessionID || state.RunID != parent.RunID {
		return nil, errors.New("harness fork boundary state does not match compiled parent turn")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item, err := h.events.Append(event.Draft{SessionID: request.SessionID, RunID: request.RunID, StageID: "session.fork", Type: "session.forked", ConfigDigest: h.doc.ConfigDigest, CausationID: boundaryEvent.Digest, CorrelationID: request.ParentSessionID, Data: map[string]any{"parent_session_id": request.ParentSessionID, "parent_run_id": boundaryEvent.RunID, "at_sequence": request.AtSequence, "parent_event_digest": boundaryEvent.Digest, "parent_messages_ref": state.MessagesRef, "parent_checkpoint": json.RawMessage(parent.State), "model_ref": state.ModelRef}})
	if err != nil {
		return nil, err
	}
	if _, err := h.events.SaveCheckpoint(h.boundaryPath(request.SessionID, request.RunID), request.SessionID, request.RunID, map[string]any{"config_digest": h.doc.ConfigDigest, "parent_session_id": request.ParentSessionID, "parent_run_id": boundaryEvent.RunID, "at_sequence": request.AtSequence, "parent_messages_ref": state.MessagesRef, "parent_checkpoint": json.RawMessage(parent.State)}); err != nil {
		return nil, err
	}
	out := make(chan Event, 1)
	out <- item
	close(out)
	return out, nil
}

func (h *Harness) Resume(ctx context.Context, request ResumeRequest) (<-chan Event, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	active := h.active[runKey(request.SessionID, request.RunID)]
	h.mu.Unlock()
	if active == nil {
		return nil, errors.New("harness resume: no live checkpoint continuation")
	}
	start, err := nextSequence(h.eventPath)
	if err != nil {
		return nil, err
	}
	_, err = h.turn.Resume(ctx, hitl.Meta{SessionID: request.SessionID, RunID: request.RunID, StageID: "hitl.resume", ConfigDigest: h.doc.ConfigDigest}, hitl.ResumeRequest{DecisionID: request.DecisionID, ExpectedVersion: request.ExpectedVersion, ReviewDigest: request.ReviewDigest, ReportDigest: request.ReportDigest, Action: request.Action, Actor: request.Actor, EditedCall: request.EditedCall})
	if err != nil {
		return nil, err
	}
	return h.stream(ctx, request.SessionID, request.RunID, start, active), nil
}

func (h *Harness) Payload(ref string) ([]byte, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	return h.payloads.Get(ref)
}

func (h *Harness) Close() error { return nil }

func (h *Harness) stream(ctx context.Context, sessionID, runID string, next uint64, active *activeRun) <-chan Event {
	out := make(chan Event, 32)
	updates, cancelWatch := h.events.Watch()
	go func() {
		defer close(out)
		defer cancelWatch()
		done := active.done
		for {
			next = sendReplay(ctx, out, h.eventPath, sessionID, runID, next)
			select {
			case <-ctx.Done():
				return
			case <-done:
				// Completion closes only after the final append is fsynced. Replay
				// once more in case the previous read overlapped that append.
				_ = sendReplay(ctx, out, h.eventPath, sessionID, runID, next)
				return
			case _, ok := <-updates:
				if !ok {
					return
				}
			}
		}
	}()
	return out
}

var replayEvents = event.Replay

func sendReplay(ctx context.Context, out chan<- Event, path, sessionID, runID string, next uint64) uint64 {
	events, err := replayEvents(path)
	if err != nil {
		return next
	}
	for _, item := range events {
		if item.Sequence < next || item.SessionID != sessionID || item.RunID != runID {
			continue
		}
		select {
		case out <- item:
			next = item.Sequence + 1
		case <-ctx.Done():
			return next
		}
	}
	return next
}

func harnessControlRoot(doc *spec.Document) (string, error) {
	return spec.ControlRootPath(doc), nil
}

func harnessProviders(doc *spec.Document, overrides map[string]api.Provider, decorators ...func(string, Provider) Provider) (map[string]turn.Provider, error) {
	result := make(map[string]turn.Provider, len(doc.Spec.Providers))
	for ref, configured := range doc.Spec.Providers {
		backend := overrides[ref]
		if backend == nil {
			key := os.Getenv(configured.Credentials.APIKey.SecretRef.Name)
			kind := api.ProviderOpenAI
			switch configured.Protocol {
			case "anthropic":
				kind = api.ProviderAnthropic
			case "gemini":
				kind = api.ProviderGemini
			case "openai-compatible":
				kind = api.ProviderOpenAI
			default:
				return nil, fmt.Errorf("harness provider %q has unsupported protocol %q", ref, configured.Protocol)
			}
			backend = api.NewProvider(&api.ProviderConfig{Kind: kind, APIKey: key, BaseURL: configured.Endpoint.Resolved})
		}
		for _, decorate := range decorators {
			if decorate != nil {
				backend = decorate(ref, backend)
			}
		}
		var adapter *provider.Adapter
		var err error
		switch configured.Protocol {
		case "anthropic":
			adapter, err = provider.NewAnthropic(backend)
		case "gemini":
			adapter, err = provider.NewGemini(backend)
		default:
			adapter, err = provider.NewOpenAICompatible(backend)
		}
		if err != nil {
			return nil, err
		}
		result[ref] = adapter
	}
	return result, nil
}

func authorizationKey(path string) ([]byte, error) {
	if key, err := os.ReadFile(path); err == nil {
		if len(key) != 32 {
			return nil, errors.New("harness authorization key has invalid length")
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func nextSequence(path string) (uint64, error) {
	events, err := event.Replay(path)
	if errors.Is(err, os.ErrNotExist) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 1, nil
	}
	return events[len(events)-1].Sequence + 1, nil
}

func runKey(sessionID, runID string) string { return sessionID + "\x00" + runID }

func sessionOf(key string) string {
	session, _, _ := strings.Cut(key, "\x00")
	return session
}

func (h *Harness) boundaryPath(sessionID, runID string) string {
	return filepath.Join(h.control, "turn-boundaries", sessionID, runID+".json")
}

type harnessTokens struct{}

func (harnessTokens) Count(value string) (int, error) { return len(value)/4 + 1, nil }
func (harnessTokens) CountMessages(value []message.Message) (int, error) {
	raw, err := json.Marshal(value)
	return len(raw)/4 + 1, err
}

type routeSummarizer struct {
	runtime *turn.Runtime
}

func (s routeSummarizer) Summarize(ctx context.Context, route, system string, messages []message.Message, previous string) (string, error) {
	_ = previous
	return s.runtime.RouteText(ctx, route, system, messages)
}

func routeSummarizerFor(runtime *turn.Runtime) routeSummarizer {
	return routeSummarizer{runtime: runtime}
}

type embeddedDelivery struct{}

func (*embeddedDelivery) Deliver(context.Context, ioctx.DeliveryRequest) error { return nil }

type embeddedRedactor struct{ values []string }

func (r embeddedRedactor) Redact(policy string, value []byte) ([]byte, error) {
	if policy != "secrets-and-control-paths" {
		return nil, fmt.Errorf("unsupported compiled redaction policy %q", policy)
	}
	result := string(value)
	for _, secret := range r.values {
		if secret != "" {
			result = strings.ReplaceAll(result, secret, "[REDACTED]")
		}
	}
	return []byte(result), nil
}

type fileDeadLetter struct{ root string }

func (f fileDeadLetter) Store(_ context.Context, request ioctx.DeadLetterRequest) error {
	path := filepath.Join(f.root, filepath.Clean(request.ControlPath), request.Delivery.EventID+".json")
	if !strings.HasPrefix(path, filepath.Clean(f.root)+string(filepath.Separator)) {
		return errors.New("dead-letter path escapes control root")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
