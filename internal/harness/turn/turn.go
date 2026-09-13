// Package turn wires compiled harness resources into the neutral pipeline runner.
package turn

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/pipeline"
	"github.com/qiangli/ycode/internal/harness/provider"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/internal/harness/stages/hitl"
	"github.com/qiangli/ycode/internal/harness/stages/ioctx"
	memoryStage "github.com/qiangli/ycode/internal/harness/stages/memory"
	memexmemory "github.com/qiangli/ycode/pkg/memex/memory"
)

type Provider interface {
	Send(context.Context, provider.Request) <-chan provider.Event
}

type QueueItem struct {
	Text string `json:"text"`
}

type Queue interface {
	Drain(context.Context, string, []string) ([]QueueItem, error)
}

type MemoryMaterializer interface {
	Materialize(context.Context, string, []message.Message) ([]*memexmemory.Memory, error)
}

type BashyBoundary interface {
	Preflight(context.Context, hitl.Meta, hitl.Call) (hitl.Preflight, error)
	Execute(context.Context, hitl.Meta, hitl.Call, string) (any, error)
}

type Config struct {
	Document    *spec.Document
	Events      *event.Store
	Payloads    *event.PayloadStore
	IO          *ioctx.Engine
	Memory      *memoryStage.Engine
	HITL        *hitl.Controller
	Bashy       BashyBoundary
	Providers   map[string]Provider
	Queue       Queue
	Materialize MemoryMaterializer
	Observer    pipeline.Observer
}

type Runtime struct {
	doc         *spec.Document
	events      *event.Store
	payloads    *event.PayloadStore
	io          *ioctx.Engine
	memory      *memoryStage.Engine
	hitl        *hitl.Controller
	bashy       BashyBoundary
	providers   map[string]Provider
	queue       Queue
	materialize MemoryMaterializer
	registry    *pipeline.Registry
	observer    pipeline.Observer
	bashyRuns   map[string]spec.BashyRunNode
	resumeMu    sync.Mutex
	resumes     map[string]chan hitl.Resolution
	early       map[string]hitl.Resolution
	activeRuns  map[string]struct{}
}

type Request struct {
	SessionID      string
	RunID          string
	AgentRef       string
	OriginFrontend string
	HumanAvailable bool
	Input          ioctx.CanonicalInput
}

func New(config Config) (*Runtime, error) {
	if config.Document == nil || config.Events == nil || config.Payloads == nil || config.IO == nil || config.Memory == nil || config.HITL == nil || config.Bashy == nil || config.Queue == nil || config.Materialize == nil {
		return nil, errors.New("turn runtime requires compiled document and all explicit mechanisms")
	}
	runtime := &Runtime{doc: config.Document, events: config.Events, payloads: config.Payloads, io: config.IO, memory: config.Memory, hitl: config.HITL, bashy: config.Bashy, providers: config.Providers, queue: config.Queue, materialize: config.Materialize, registry: pipeline.NewRegistry(), observer: config.Observer, resumes: make(map[string]chan hitl.Resolution), early: make(map[string]hitl.Resolution), activeRuns: make(map[string]struct{})}
	bashyRuns, err := compileBashyRunIndex(config.Document)
	if err != nil {
		return nil, err
	}
	runtime.bashyRuns = bashyRuns
	if err := runtime.register(); err != nil {
		return nil, err
	}
	return runtime, nil
}

// Resume resolves a live, checkpointed HITL continuation. It never reruns the
// pipeline from its entry node: the suspended graph resumes at hitl.review.
// A process restart cannot reconstruct the Go stack from a checkpoint, so a
// restored-but-not-live decision is rejected without consuming its one use.
func (r *Runtime) Resume(ctx context.Context, meta hitl.Meta, request hitl.ResumeRequest) (hitl.Resolution, error) {
	r.resumeMu.Lock()
	_, live := r.activeRuns[turnRunKey(meta.SessionID, meta.RunID)]
	r.resumeMu.Unlock()
	if !live {
		return hitl.Resolution{}, errors.New("turn resume: checkpoint has no live continuation")
	}
	resolution, err := r.hitl.Resume(ctx, meta, request)
	if err != nil {
		return hitl.Resolution{}, err
	}
	r.resumeMu.Lock()
	waiter := r.resumes[request.DecisionID]
	if waiter == nil {
		r.early[request.DecisionID] = resolution
	}
	r.resumeMu.Unlock()
	if waiter == nil {
		return resolution, nil
	}
	select {
	case waiter <- resolution:
		return resolution, nil
	case <-ctx.Done():
		return hitl.Resolution{}, ctx.Err()
	}
}

func (r *Runtime) Run(ctx context.Context, request Request) (ioctx.Output, error) {
	if request.SessionID == "" || request.RunID == "" || request.OriginFrontend == "" {
		return ioctx.Output{}, errors.New("turn run requires session, run and origin frontend")
	}
	r.resumeMu.Lock()
	r.activeRuns[turnRunKey(request.SessionID, request.RunID)] = struct{}{}
	r.resumeMu.Unlock()
	defer func() {
		r.resumeMu.Lock()
		delete(r.activeRuns, turnRunKey(request.SessionID, request.RunID))
		r.resumeMu.Unlock()
	}()
	agentRef := request.AgentRef
	if agentRef == "" {
		agentRef = r.doc.Spec.Runtime.DefaultAgentRef
	}
	agent, ok := r.doc.Spec.Agents[agentRef]
	if !ok {
		return ioctx.Output{}, fmt.Errorf("turn: undeclared agent %q", agentRef)
	}
	state := pipeline.NewState()
	entry := r.doc.Spec.Pipelines[agent.PipelineRef]
	if err := state.SetTyped("request", entry.Inputs["request"], request.Input); err != nil {
		return ioctx.Output{}, err
	}
	// The neutral runner treats outputs as graph ports; seed their declarations
	// without values so stage writes remain typed.
	for name, typ := range entry.Outputs {
		if err := state.Declare(name, typ); err != nil {
			return ioctx.Output{}, err
		}
	}
	values := context.WithValue(ctx, runKey{}, runContext{sessionID: request.SessionID, runID: request.RunID, agentRef: agentRef, originFrontend: request.OriginFrontend, humanAvailable: request.HumanAvailable})
	runner := pipeline.NewRunner(r.registry).WithPipelines(r.doc.Spec.Pipelines).WithHooks(r.doc.Spec.Hooks).WithObserver(r.observer)
	if err := runner.RunPipeline(values, agent.PipelineRef, state); err != nil {
		return ioctx.Output{}, err
	}
	value, _, _, ok := state.Read("output")
	if !ok {
		return ioctx.Output{}, errors.New("turn: compiled pipeline produced no output")
	}
	output, ok := value.(ioctx.Output)
	if !ok {
		return ioctx.Output{}, fmt.Errorf("turn: output has type %T", value)
	}
	return output, nil
}

func turnRunKey(sessionID, runID string) string { return sessionID + "\x00" + runID }

type runKey struct{}
type runContext struct {
	sessionID, runID, agentRef, originFrontend string
	humanAvailable                             bool
}

func runFrom(ctx context.Context) (runContext, error) {
	value, ok := ctx.Value(runKey{}).(runContext)
	if !ok {
		return runContext{}, errors.New("turn: missing run context")
	}
	return value, nil
}

func (r *Runtime) meta(ctx context.Context, stage string) (ioctx.Meta, error) {
	run, err := runFrom(ctx)
	if err != nil {
		return ioctx.Meta{}, err
	}
	return ioctx.Meta{SessionID: run.sessionID, RunID: run.runID, StageID: stage, ConfigDigest: r.doc.ConfigDigest}, nil
}

func fail(err error) pipeline.Outcome { return pipeline.Failure("stage", false, err) }

func (r *Runtime) add(name string, handler pipeline.Handler) error {
	return r.registry.Register(pipeline.Definition{Name: name, Handler: handler})
}

func (r *Runtime) register() error {
	registrations := map[string]pipeline.Handler{
		"lifecycle.transition":                 r.lifecycle,
		"input.normalize":                      r.input,
		"context.load":                         r.context,
		"memory.recall":                        r.recall,
		"prompt.assemble":                      r.assemble,
		"checkpoint.save":                      r.checkpoint,
		"queue.drain":                          r.drain,
		"messages.apply-input":                 r.applyInput,
		"context.measure":                      r.measure,
		"state.forward":                        passthrough("value"),
		"messages.append-source":               r.appendSource,
		"llm.call":                             r.callModel,
		"messages.normalize-provider-response": r.normalize,
		"messages.append-assistant":            r.appendAssistant,
		"loop.finish":                          r.finish,
		"memory.write":                         r.writeMemory,
		"memory.compact":                       r.compact,
		"output.emit":                          r.output,
		"outcome.fail": func(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
			return pipeline.Failure(text(in.With["class"]), false, errors.New(text(in.With["class"])))
		},
		"bashy.preflight":              r.preflight,
		"bashy.run":                    r.bashyRun,
		"policy.evaluate":              r.evaluate,
		"bashy.execute":                r.execute,
		"bashy.deny":                   r.deny,
		"bashy.reject":                 r.reject,
		"command.initialize":           r.commandInitialize,
		"command.finish":               r.commandFinish,
		"command.apply-edit":           r.commandApplyEdit,
		"messages.append-tool-results": r.appendToolResults,
		"messages.clear-tool-results":  r.clearToolResults,
		"hitl.review":                  r.review,
		"policy.bind-approval":         r.bindApproval,
		"state.project":                passthrough("value"),
		"event.annotate":               passthrough("value"),
		"lock.acquire":                 r.lockAcquire,
		"lock.release":                 func(context.Context, pipeline.Invocation) pipeline.Outcome { return pipeline.Success(nil) },
	}
	for name, handler := range registrations {
		if err := r.add(name, handler); err != nil {
			return err
		}
	}
	return nil
}

func passthrough(port string) pipeline.Handler {
	return func(_ context.Context, in pipeline.Invocation) pipeline.Outcome {
		return pipeline.Success(map[string]any{port: in.Inputs[port]})
	}
}

func text(value any) string { result, _ := value.(string); return result }

func stringList(value any) []string {
	if typed, ok := value.([]string); ok {
		return append([]string(nil), typed...)
	}
	var out []string
	if typed, ok := value.([]any); ok {
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func checkpointPath(doc *spec.Document, sessionID, runID string) string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = doc.BaseDir
	}
	root := filepath.Join(base, doc.Spec.Runtime.ControlRoot.PlatformDataDir)
	return filepath.Join(root, "turns", sessionID, runID+".json")
}
