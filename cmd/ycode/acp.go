package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	harnessacp "github.com/qiangli/ycode/internal/harness/acp"
	harnessspec "github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
	coreacp "github.com/qiangli/yoke/pkg/acp"
)

type harnessApp interface {
	Run(context.Context, public.RunRequest) (<-chan public.Event, error)
	Fork(context.Context, public.ForkRequest) (<-chan public.Event, error)
	Payload(string) ([]byte, error)
	Close() error
}

type acpAppFactory func(cwd string) (harnessApp, error)

type acpRunner struct {
	newApp acpAppFactory
	store  *harnessacp.Store
	stderr io.Writer
	mu     sync.Mutex
	apps   map[string]harnessApp
	route  harnessspec.CLIDispatch
}

func newACPRunner(factory acpAppFactory, stderr io.Writer, stores ...*harnessacp.Store) *acpRunner {
	store := harnessacp.NewMemory()
	if len(stores) != 0 && stores[0] != nil {
		store = stores[0]
	}
	return &acpRunner{newApp: factory, store: store, stderr: stderr, apps: make(map[string]harnessApp)}
}

func (r *acpRunner) Run(ctx context.Context, req coreacp.TurnRequest) (coreacp.TurnResponse, error) {
	prompt := joinACPPrompt(req.Prompt)
	if strings.TrimSpace(prompt) == "" {
		return coreacp.TurnResponse{}, errors.New("ACP prompt contains no text")
	}
	session, err := r.store.Resume(req.SessionID, req.Cwd)
	if err != nil {
		return coreacp.TurnResponse{}, err
	}
	app, err := r.app(session.Cwd)
	if err != nil {
		return coreacp.TurnResponse{}, err
	}
	runID, err := r.store.NextTurn(session.ID)
	if err != nil {
		return coreacp.TurnResponse{}, err
	}
	body, err := json.Marshal(map[string]string{"request": prompt})
	if err != nil {
		return coreacp.TurnResponse{}, err
	}
	stream, err := app.Run(ctx, public.RunRequest{SessionID: session.ID, RunID: runID, TriggerRef: r.route.TriggerRef, FrontendRef: r.route.FrontendRef, AgentRef: r.route.AgentRef, Principal: "acp-client", IdempotencyKey: runID, HumanAvailable: true, Body: body})
	if err != nil {
		return coreacp.TurnResponse{}, err
	}
	response := coreacp.TurnResponse{StopReason: coreacp.StopReasonEndTurn}
	var head uint64
	for item := range stream {
		head = item.Sequence
		if item.Type == "output.emitted" {
			ref, err := outputPayload(item)
			if err != nil {
				return coreacp.TurnResponse{}, err
			}
			content, err := app.Payload(ref)
			if err != nil {
				return coreacp.TurnResponse{}, err
			}
			response.Text = string(content)
		}
		if item.Type == "turn.failed" {
			return coreacp.TurnResponse{}, errors.New("ACP harness turn failed")
		}
	}
	if err := ctx.Err(); err != nil {
		return coreacp.TurnResponse{StopReason: coreacp.StopReasonCancelled}, err
	}
	if response.Text == "" {
		return coreacp.TurnResponse{}, errors.New("ACP harness produced no canonical output")
	}
	if err := r.store.Advance(session.ID, head); err != nil {
		return coreacp.TurnResponse{}, err
	}
	return response, nil
}

func (r *acpRunner) NewSession(_ context.Context, cwd string) (coreacp.Session, error) {
	value, err := r.store.Create(cwd)
	return toCoreSession(value), err
}
func (r *acpRunner) ResumeSession(_ context.Context, id, cwd string) (coreacp.Session, error) {
	value, err := r.store.Resume(id, cwd)
	return toCoreSession(value), err
}
func (r *acpRunner) ListSessions(_ context.Context, cwd string) ([]coreacp.Session, error) {
	values := r.store.List(cwd)
	out := make([]coreacp.Session, len(values))
	for i := range values {
		out[i] = toCoreSession(values[i])
	}
	return out, nil
}
func (r *acpRunner) CloseSession(_ context.Context, id string) error { return r.store.Close(id) }
func (r *acpRunner) ForkSession(ctx context.Context, id, cwd string) (coreacp.Session, error) {
	parent, err := r.store.Resume(id, cwd)
	if err != nil {
		return coreacp.Session{}, err
	}
	if parent.HeadSequence == 0 {
		return coreacp.Session{}, errors.New("ACP fork requires a completed turn boundary")
	}
	value, err := r.store.Fork(id, cwd, parent.HeadSequence)
	if err != nil {
		return coreacp.Session{}, err
	}
	app, err := r.app(parent.Cwd)
	if err != nil {
		return coreacp.Session{}, err
	}
	stream, err := app.Fork(ctx, public.ForkRequest{ParentSessionID: parent.ID, SessionID: value.ID, RunID: "fork-" + value.ID, AtSequence: parent.HeadSequence})
	if err != nil {
		return coreacp.Session{}, err
	}
	head := parent.HeadSequence
	for item := range stream {
		head = item.Sequence
	}
	if err := ctx.Err(); err != nil {
		return coreacp.Session{}, err
	}
	if err := r.store.Advance(value.ID, head); err != nil {
		return coreacp.Session{}, err
	}
	return toCoreSession(value), nil
}

func toCoreSession(value harnessacp.Session) coreacp.Session {
	return coreacp.Session{ID: value.ID, Cwd: value.Cwd, UpdatedAt: value.UpdatedAt}
}

func (r *acpRunner) app(cwd string) (harnessApp, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if app := r.apps[cwd]; app != nil {
		return app, nil
	}
	app, err := r.newApp(cwd)
	if err != nil {
		return nil, fmt.Errorf("load agent.yaml: %w", err)
	}
	r.apps[cwd] = app
	return app, nil
}
func (r *acpRunner) Close() error {
	r.mu.Lock()
	apps := r.apps
	r.apps = make(map[string]harnessApp)
	r.mu.Unlock()
	var first error
	for _, app := range apps {
		if err := app.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func outputPayload(item public.Event) (string, error) {
	var body struct {
		Deliveries []struct {
			PayloadRef string `json:"payload_ref"`
		} `json:"deliveries"`
	}
	if err := json.Unmarshal(item.Data, &body); err != nil {
		return "", err
	}
	if len(body.Deliveries) != 1 || body.Deliveries[0].PayloadRef == "" {
		return "", errors.New("ACP output requires exactly one canonical delivery")
	}
	return body.Deliveries[0].PayloadRef, nil
}
func joinACPPrompt(blocks []coreacp.ContentBlock) string {
	var prompt strings.Builder
	for _, block := range blocks {
		prompt.WriteString(block.Text)
	}
	return prompt.String()
}

func serveACP(input io.Reader, output, stderr io.Writer, factory acpAppFactory, stores ...*harnessacp.Store) error {
	return serveACPWithRoute(input, output, stderr, factory, harnessspec.CLIDispatch{}, stores...)
}

func serveACPWithRoute(input io.Reader, output, stderr io.Writer, factory acpAppFactory, route harnessspec.CLIDispatch, stores ...*harnessacp.Store) error {
	runner := newACPRunner(factory, stderr, stores...)
	runner.route = route
	defer runner.Close()
	agent := coreacp.NewAgent(runner, coreacp.AgentOptions{}, input, output)
	<-agent.Done()
	return nil
}

var _ coreacp.SessionLifecycle = (*acpRunner)(nil)
