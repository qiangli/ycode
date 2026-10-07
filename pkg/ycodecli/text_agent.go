package ycodecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// TextAgent is a script-owned session of the compiled YAML harness. It uses
// the authored CLI input route and canonical events, including durable usage.
// Closing cancels active work and waits for its terminal event; evidence stays.
type TextAgent struct {
	app     *harnessApplication
	route   spec.CLIDispatch
	session string
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	closed  bool
}

func textAgentDocument(source string, data []byte) (*spec.Document, error) {
	doc, err := spec.Compile(source, data)
	if err != nil {
		return nil, err
	}
	route := doc.Spec.Interfaces.CLI.Root.Dispatch
	if route == nil || route.Operation != "input" || doc.Spec.Frontends[route.FrontendRef].Kind != "one-shot" {
		return nil, errors.New("agent spec requires a configured local input route at the CLI root")
	}
	return doc, nil
}

// ValidateTextAgent performs strict compilation without creating runtime state.
func ValidateTextAgent(source string, data []byte) error {
	_, err := textAgentDocument(source, data)
	return err
}

func OpenTextAgent(source string, data []byte, options ...public.LoadOption) (*TextAgent, error) {
	open, err := PrepareTextAgent(source, data)
	if err != nil {
		return nil, err
	}
	return open(options...)
}

// PrepareTextAgent binds routing and runtime to the same immutable digest.
// No import or policy file is reread when the authorized call starts.
func PrepareTextAgent(source string, data []byte) (func(...public.LoadOption) (*TextAgent, error), error) {
	doc, err := textAgentDocument(source, data)
	if err != nil {
		return nil, err
	}
	prepared, err := public.PrepareSource(source, data)
	if err != nil {
		return nil, err
	}
	if prepared.Digest() != doc.ConfigDigest {
		return nil, errors.New("agent definition changed during preparation")
	}
	return func(options ...public.LoadOption) (*TextAgent, error) {
		options = append(append([]public.LoadOption{}, options...), public.WithHarnessProviderDecorator(agentFenceMeterProvider))
		harness, err := prepared.Load(options...)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithCancel(context.Background())
		return &TextAgent{app: &harnessApplication{doc: doc, harness: harness}, route: *doc.Spec.Interfaces.CLI.Root.Dispatch,
			session: uuid.NewString(), ctx: ctx, cancel: cancel}, nil
	}, nil
}

func (a *TextAgent) Run(ctx context.Context, instruction string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return "", errors.New("agent session is closed")
	}
	if strings.TrimSpace(instruction) == "" {
		return "", errors.New("agent instruction is empty")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.ctx, cancel)
	defer stop()
	defer cancel()
	body, err := json.Marshal(map[string]string{a.route.Input.PayloadKey: instruction})
	if err != nil {
		return "", err
	}
	limit := a.app.doc.Spec.Frontends[a.route.FrontendRef].Limits.MaxInputBytes
	if limit > 0 && len(body) > limit {
		return "", fmt.Errorf("frontend input exceeds %d bytes", limit)
	}
	run := uuid.NewString()
	events, err := a.app.harness.Run(ctx, public.RunRequest{SessionID: a.session, RunID: run,
		TriggerRef: a.route.TriggerRef, FrontendRef: a.route.FrontendRef, AgentRef: a.route.AgentRef,
		Principal: localPrincipal(), IdempotencyKey: run, Body: body, HumanAvailable: false})
	if err != nil {
		return "", err
	}
	var output bytes.Buffer
	renderer := a.app.renderer(&output)
	for item := range events {
		if err := renderer.Render(item); err != nil {
			return output.String(), err
		}
	}
	return output.String(), ctx.Err()
}

func (a *TextAgent) Close() error {
	a.cancel()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !a.app.harness.Settle(ctx, a.session) {
		return errors.New("agent session cleanup timed out")
	}
	a.closed = true
	return a.app.Close()
}
