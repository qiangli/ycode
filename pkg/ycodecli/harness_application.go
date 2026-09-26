package ycodecli

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/frontend"
	"github.com/qiangli/ycode/internal/harness/spec"
	public "github.com/qiangli/ycode/pkg/ycode"
)

// harnessApplication is the CLI composition root. Every presentation surface
// receives the same compiled document, public Harness and durable event stream.
// No command is allowed to construct providers, tools, policy or persistence.
type harnessApplication struct {
	doc     *spec.Document
	harness *public.Harness
}

func openHarnessApplication(path string, options ...public.LoadOption) (*harnessApplication, error) {
	doc, err := spec.Load(path)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}
	harness, err := public.Load(path, options...)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &harnessApplication{doc: doc, harness: harness}, nil
}

func (a *harnessApplication) Close() error { return a.harness.Close() }

func (a *harnessApplication) RunText(ctx context.Context, frontendRef, sessionID, text string, output io.Writer) error {
	body, err := encodePrompt(text)
	if err != nil {
		return err
	}
	local, err := frontend.NewLocal(a.doc, frontendRef, a)
	if err != nil {
		return err
	}
	defaults := a.defaults()
	if sessionID != "" {
		defaults.SessionID = sessionID
	}
	return local.Run(ctx, frontend.Input{SessionID: defaults.SessionID, AgentRef: defaults.AgentRef, Principal: defaults.Principal, Body: body}, a.renderer(output))
}

func (a *harnessApplication) RunReader(ctx context.Context, frontendRef, sessionID string, input io.Reader, output io.Writer) error {
	configured, ok := a.doc.Spec.Frontends[frontendRef]
	if !ok {
		return fmt.Errorf("frontend %q is not declared", frontendRef)
	}
	limit := int64(configured.Limits.MaxInputBytes)
	if limit <= 0 {
		limit = 1 << 20
	}
	raw, err := io.ReadAll(io.LimitReader(input, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("frontend input exceeds %d bytes", limit)
	}
	return a.RunText(ctx, frontendRef, sessionID, strings.TrimSuffix(string(raw), "\n"), output)
}

func (a *harnessApplication) RunREPL(ctx context.Context, frontendRef, sessionID string, input io.Reader, output io.Writer) error {
	local, err := frontend.NewLocal(a.doc, frontendRef, a)
	if err != nil {
		return err
	}
	configured := a.doc.Spec.Frontends[frontendRef]
	defaults := a.defaults()
	if sessionID != "" {
		defaults.SessionID = sessionID
	}
	scanner := bufio.NewScanner(input)
	if configured.Limits.MaxInputBytes > 0 {
		scanner.Buffer(make([]byte, 4096), configured.Limits.MaxInputBytes)
	}
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		body, err := encodePrompt(scanner.Text())
		if err != nil {
			return err
		}
		if err := local.Run(ctx, frontend.Input{SessionID: defaults.SessionID, AgentRef: defaults.AgentRef, Principal: defaults.Principal, Body: body}, a.renderer(output)); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (a *harnessApplication) defaults() frontend.Defaults {
	return frontend.Defaults{SessionID: uuid.NewString(), Principal: localPrincipal()}
}

func localPrincipal() string {
	if current, err := user.Current(); err == nil && current.Uid != "" {
		return "local:" + current.Uid
	}
	return "local-user"
}

func (a *harnessApplication) Submit(ctx context.Context, input frontend.Input) (<-chan event.Event, error) {
	triggerRef, err := a.triggerFor(input.FrontendRef)
	if err != nil {
		return nil, err
	}
	if input.SessionID == "" {
		input.SessionID = uuid.NewString()
	}
	if input.Principal == "" {
		input.Principal = localPrincipal()
	}
	runID := uuid.NewString()
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = runID
	}
	configured := a.doc.Spec.Frontends[input.FrontendRef]
	return a.harness.Run(ctx, public.RunRequest{
		SessionID: input.SessionID, RunID: runID, TriggerRef: triggerRef,
		FrontendRef: input.FrontendRef, AgentRef: input.AgentRef,
		Principal: input.Principal, IdempotencyKey: input.IdempotencyKey,
		Body: input.Body, HumanAvailable: configured.HITL,
	})
}

func (a *harnessApplication) Resume(context.Context, frontend.Resume) (<-chan event.Event, error) {
	return nil, errors.New("interactive approval must use the typed Harness.Resume request")
}

func (a *harnessApplication) Fork(ctx context.Context, input frontend.Fork) (<-chan event.Event, error) {
	return a.harness.Fork(ctx, public.ForkRequest{
		ParentSessionID: input.SessionID, SessionID: uuid.NewString(),
		RunID: uuid.NewString(), AtSequence: input.AtSequence,
	})
}

func (a *harnessApplication) triggerFor(frontendRef string) (string, error) {
	var matches []string
	for ref, trigger := range a.doc.Spec.Triggers {
		for _, candidate := range trigger.FrontendRefs {
			if candidate == frontendRef {
				matches = append(matches, ref)
				break
			}
		}
	}
	sort.Strings(matches)
	if len(matches) != 1 {
		return "", fmt.Errorf("frontend %q must route through exactly one trigger (found %d)", frontendRef, len(matches))
	}
	return matches[0], nil
}

func (a *harnessApplication) renderer(output io.Writer) frontend.Renderer {
	return frontend.RenderFunc(func(item event.Event) error {
		switch item.Type {
		case "output.emitted":
			ref, err := outputPayload(item)
			if err != nil {
				return err
			}
			payload, err := a.harness.Payload(ref)
			if err != nil {
				return err
			}
			_, err = output.Write(payload)
			if err == nil && len(payload) != 0 && payload[len(payload)-1] != '\n' {
				_, err = io.WriteString(output, "\n")
			}
			return err
		case "turn.failed":
			var failure struct {
				Error string `json:"error"`
			}
			if json.Unmarshal(item.Data, &failure) == nil && failure.Error != "" {
				return errors.New(failure.Error)
			}
			return errors.New("harness turn failed")
		default:
			return nil
		}
	})
}

// envAuthenticator enforces the secret reference compiled for a network
// frontend. An absent secret or credential always denies admission.
type envAuthenticator struct{}

func (envAuthenticator) Authenticate(_ context.Context, _ string, auth spec.FrontendAuth, credential string) (string, error) {
	want := os.Getenv(auth.SecretRef.Name)
	credential = strings.TrimSpace(strings.TrimPrefix(credential, "Bearer "))
	if want == "" || credential == "" || subtle.ConstantTimeCompare([]byte(want), []byte(credential)) != 1 {
		return "", errors.New("invalid frontend credential")
	}
	return "network:" + auth.SecretRef.Name, nil
}

var _ frontend.Controller = (*harnessApplication)(nil)

// Payload lets network frontends return delivered output text.
func (a *harnessApplication) Payload(ref string) ([]byte, error) { return a.harness.Payload(ref) }
