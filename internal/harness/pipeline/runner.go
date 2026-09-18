package pipeline

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/yoke/pkg/dag"
)

type Runner struct {
	registry  *Registry
	pipelines map[string]spec.Pipeline
	hooks     map[string]spec.Hook
	observer  Observer
}

type executionKey struct{}
type executionScope struct {
	mu     sync.Mutex
	counts map[string]int
	active map[string]int
}

type Operation struct {
	Kind       string
	Name       string
	Pipeline   string
	Stage      string
	Attributes map[string]string
}

// Observer is the policy-free telemetry seam. Implementations must not record
// model-visible content unless the compiled observability resource allows it.
type Observer interface {
	Start(context.Context, Operation) (context.Context, func(Outcome))
}

func NewRunner(registry *Registry) *Runner {
	return &Runner{registry: registry, pipelines: map[string]spec.Pipeline{}, hooks: map[string]spec.Hook{}}
}
func (r *Runner) WithPipelines(v map[string]spec.Pipeline) *Runner { r.pipelines = v; return r }
func (r *Runner) WithHooks(v map[string]spec.Hook) *Runner         { r.hooks = v; return r }
func (r *Runner) WithObserver(v Observer) *Runner                  { r.observer = v; return r }

func (r *Runner) RunPipeline(ctx context.Context, name string, state *State) error {
	ctx = withExecutionScope(ctx)
	out := r.runPipeline(ctx, name, state)
	return out.Err
}
func (r *Runner) RunPipelineOutcome(ctx context.Context, name string, state *State) Outcome {
	ctx = withExecutionScope(ctx)
	return r.runPipeline(ctx, name, state)
}

func (r *Runner) runPipeline(ctx context.Context, name string, state *State) (result Outcome) {
	ctx, finish := r.observe(ctx, Operation{Kind: "pipeline", Name: name, Pipeline: name})
	defer func() { finish(result) }()
	p, ok := r.pipelines[name]
	if !ok {
		result = Failure("unknown-pipeline", false, fmt.Errorf("pipeline: pipeline %q is not declared", name))
		return
	}
	for slot, typ := range p.Inputs {
		if err := state.Declare(slot, typ); err != nil {
			result = Failure("state", false, err)
			return
		}
	}
	for slot, decl := range p.State {
		if err := state.Declare(slot, decl.Type); err != nil {
			result = Failure("state", false, err)
			return
		}
	}
	for slot, typ := range p.Outputs {
		if err := state.Declare(slot, typ); err != nil {
			result = Failure("state", false, err)
			return
		}
	}
	ex := &graphExecutor{runner: r, pipeline: p, state: state, outcomes: map[string]Outcome{}, byID: map[string]spec.Stage{}}
	doc := &dag.Document{}
	for _, node := range p.Nodes {
		doc.Tasks = append(doc.Tasks, &dag.Task{Name: node.ID, Requires: node.Needs})
		doc.Order = append(doc.Order, node.ID)
		ex.nodes = append(ex.nodes, node)
		ex.byID[node.ID] = node
	}
	graph, err := dag.BuildGraph(doc)
	if err != nil {
		result = Failure("invalid-dag", false, err)
		return
	}
	concurrency := p.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	_, err = (&dag.Engine{Graph: graph, Concurrency: concurrency, FailFast: false, Executor: ex}).Run(ctx)
	if err != nil {
		result = Failure("scheduler", false, err)
		return
	}
	for _, node := range p.Nodes {
		out := ex.get(node.ID)
		if out.Class == OutcomeFailed || out.Class == OutcomeCancelled {
			result = out
			return
		}
	}
	result = Outcome{Class: OutcomeSucceeded}
	return
}

type graphExecutor struct {
	runner   *Runner
	pipeline spec.Pipeline
	state    *State
	mu       sync.RWMutex
	outcomes map[string]Outcome
	nodes    []spec.Stage
	byID     map[string]spec.Stage
}

func (x *graphExecutor) Execute(ctx context.Context, t *dag.Task, _ dag.TaskIO) dag.TaskResult {
	n := x.byID[t.Name]
	if x.pipeline.FailFast && x.hasTerminalFailure() && !contains(n.RunOn, OutcomeFailed) && !contains(n.RunOn, OutcomeCancelled) {
		x.put(n.ID, Outcome{Class: OutcomeSkipped, Code: "fail-fast"})
		return dag.TaskResult{Name: t.Name, Status: dag.StatusDone}
	}
	for _, dep := range n.Needs {
		o := x.get(dep)
		if o.Class == OutcomeSkipped && !contains(n.AcceptsSkipped, dep) {
			x.put(n.ID, Outcome{Class: OutcomeSkipped, Code: "dependency-skipped"})
			return dag.TaskResult{Name: t.Name, Status: dag.StatusDone}
		}
		if (o.Class == OutcomeFailed || o.Class == OutcomeObserved) && !contains(n.RunOn, o.Class) {
			x.put(n.ID, Outcome{Class: OutcomeSkipped, Code: "dependency-outcome"})
			return dag.TaskResult{Name: t.Name, Status: dag.StatusDone}
		}
	}
	if n.WhenExpr != nil {
		ok, err := evalExpression(*n.WhenExpr, x.state, nil)
		if err != nil {
			x.put(n.ID, Failure("when", false, err))
			return dag.TaskResult{Name: t.Name, Status: dag.StatusDone}
		}
		if !ok {
			x.put(n.ID, Outcome{Class: OutcomeSkipped, Code: "when-false"})
			return dag.TaskResult{Name: t.Name, Status: dag.StatusDone}
		}
	}
	out := x.runner.runNode(ctx, n, x.state)
	x.put(n.ID, out)
	return dag.TaskResult{Name: t.Name, Status: dag.StatusDone}
}
func (x *graphExecutor) hasTerminalFailure() bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, outcome := range x.outcomes {
		if outcome.Class == OutcomeFailed || outcome.Class == OutcomeCancelled {
			return true
		}
	}
	return false
}
func (x *graphExecutor) get(id string) Outcome {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.outcomes[id]
}
func (x *graphExecutor) put(id string, o Outcome) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.outcomes[id] = o
}

func (r *Runner) runNode(ctx context.Context, n spec.Stage, state *State) (result Outcome) {
	operation := Operation{Kind: "stage", Name: n.ID, Stage: runKind(n.Run)}
	ctx, finish := r.observe(ctx, operation)
	defer func() { finish(result) }()
	attempts := 1
	if n.RetryV1 != nil {
		attempts = n.RetryV1.MaxAttempts
	}
	var out Outcome
	for attempt := 1; attempt <= attempts; attempt++ {
		out = r.runForm(ctx, n.ID, n.Run, state)
		if out.Class != OutcomeFailed {
			result = out
			return
		}
		if n.RetryV1 == nil || attempt == attempts {
			result = out
			return
		}
		retry, err := evalExpression(n.RetryV1.When, state, map[string]any{"outcome": map[string]any{"class": out.Class, "code": out.Code, "retryable": out.Retryable}, "error": map[string]any{"code": out.Code, "retryable": out.Retryable}})
		if err != nil {
			result = Failure("retry-expression", false, err)
			return
		}
		if !retry {
			result = out
			return
		}
		d := backoff(n.RetryV1.Backoff, attempt)
		if d > 0 {
			select {
			case <-ctx.Done():
				result = Failure("cancelled", false, ctx.Err())
				return
			case <-time.After(d):
			}
		}
	}
	result = out
	return
}

func (r *Runner) observe(ctx context.Context, operation Operation) (context.Context, func(Outcome)) {
	if r.observer == nil {
		return ctx, func(Outcome) {}
	}
	return r.observer.Start(ctx, operation)
}

func runKind(run spec.Run) string {
	switch {
	case run.Stage != "":
		return run.Stage
	case run.PipelineRef != "":
		return "pipeline.call"
	case run.Repeat != nil:
		return "repeat"
	case run.ForEach != nil:
		return "forEach"
	case run.Switch != nil:
		return "switch"
	case run.Fallback != nil:
		return "fallback"
	case run.HookInvoke != "":
		return "hook.invoke"
	default:
		return "unknown"
	}
}

func (r *Runner) runForm(ctx context.Context, id string, run spec.Run, state *State) Outcome {
	switch {
	case run.Stage != "":
		return r.runStage(ctx, id, run, state)
	case run.PipelineRef != "":
		return r.runCall(ctx, run.PipelineRef, run.In, run.Out, state)
	case run.Repeat != nil:
		return r.runRepeat(ctx, *run.Repeat, state)
	case run.ForEach != nil:
		return r.runForEach(ctx, *run.ForEach, state)
	case run.Switch != nil:
		return r.runSwitch(ctx, *run.Switch, state)
	case run.Fallback != nil:
		return r.runFallback(ctx, *run.Fallback, state)
	case run.HookInvoke != "":
		h, ok := r.hooks[run.HookInvoke]
		if !ok {
			return Failure("unknown-hook", false, fmt.Errorf("pipeline: hook %q is not declared", run.HookInvoke))
		}
		release, err := enterHook(ctx, run.HookInvoke, h)
		if err != nil {
			return Failure("hook-limit", false, err)
		}
		defer release()
		out := r.runCall(ctx, h.PipelineRef, run.In, run.Out, state)
		if out.Class == OutcomeFailed && h.Failure == "continue" {
			return Outcome{Class: OutcomeObserved, Code: "hook-failure-continued", Outputs: out.Outputs}
		}
		return out
	default:
		return Failure("empty-run", false, fmt.Errorf("pipeline: node %q has no run form", id))
	}
}

func withExecutionScope(ctx context.Context) context.Context {
	if ctx.Value(executionKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, executionKey{}, &executionScope{counts: map[string]int{}, active: map[string]int{}})
}

func enterHook(ctx context.Context, name string, hook spec.Hook) (func(), error) {
	scope, ok := ctx.Value(executionKey{}).(*executionScope)
	if !ok {
		return func() {}, nil
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if hook.MaxInvocations <= 0 || scope.counts[name] >= hook.MaxInvocations {
		return nil, fmt.Errorf("pipeline: hook %q exceeded maxInvocations %d", name, hook.MaxInvocations)
	}
	if !hook.Reentrant && scope.active[name] > 0 {
		return nil, fmt.Errorf("pipeline: hook %q is not reentrant", name)
	}
	scope.counts[name]++
	scope.active[name]++
	return func() {
		scope.mu.Lock()
		scope.active[name]--
		scope.mu.Unlock()
	}, nil
}

func (r *Runner) runStage(ctx context.Context, id string, run spec.Run, state *State) Outcome {
	def, err := r.registry.lookup(run.Stage)
	if err != nil {
		return Failure("unknown-stage", false, err)
	}
	inputs := map[string]any{}
	for port, path := range run.In {
		v, _, _, ok := state.Read(path)
		if !ok {
			return Failure("missing-input", false, fmt.Errorf("pipeline: node %q input %q reads missing state %q", id, port, path))
		}
		inputs[port] = v
	}
	out := def.Handler(ctx, Invocation{StageID: id, Inputs: inputs, With: cloneMap(run.With)})
	if out.Class == "" {
		out.Class = OutcomeSucceeded
	}
	if out.Class == OutcomeFailed {
		return out
	}
	writes := map[string]any{}
	for port, path := range run.Out {
		v, ok := out.Outputs[port]
		if !ok {
			return Failure("missing-output", false, fmt.Errorf("pipeline: stage %q did not produce output %q", run.Stage, port))
		}
		writes[path] = v
	}
	if err := state.apply(writes); err != nil {
		return Failure("state-write", false, err)
	}
	return out
}

func (r *Runner) runCall(ctx context.Context, name string, in, out map[string]string, parent *State) Outcome {
	p, ok := r.pipelines[name]
	if !ok {
		return Failure("unknown-pipeline", false, fmt.Errorf("pipeline: pipeline %q is not declared", name))
	}
	child := NewState()
	for port, path := range in {
		v, _, _, found := parent.Read(path)
		if !found {
			return Failure("missing-input", false, fmt.Errorf("pipeline: call %q reads missing state %q", name, path))
		}
		typ := p.Inputs[port]
		if err := child.SetTyped(port, typ, v); err != nil {
			return Failure("state", false, err)
		}
	}
	result := r.runPipeline(ctx, name, child)
	if result.Class == OutcomeFailed {
		return result
	}
	writes := map[string]any{}
	for port, path := range out {
		v, _, _, found := child.Read(port)
		if !found {
			return Failure("missing-output", false, fmt.Errorf("pipeline: call %q did not produce %q", name, port))
		}
		writes[path] = v
	}
	if err := parent.apply(writes); err != nil {
		return Failure("state-write", false, err)
	}
	return result
}

func (r *Runner) runRepeat(ctx context.Context, x spec.RepeatRun, state *State) Outcome {
	in := cloneMapString(x.In)
	for port, path := range x.Carry {
		in[port] = path
	}
	for i := 0; i < x.MaxIterations; i++ {
		out := r.runCall(ctx, x.PipelineRef, in, x.Out, state)
		if out.Class == OutcomeFailed {
			return out
		}
		ok, err := evalExpression(x.Until, state, nil)
		if err != nil {
			return Failure("repeat-expression", false, err)
		}
		if ok {
			return out
		}
		for port, path := range x.Carry {
			in[port] = path
		}
	}
	return Failure("repeat-limit", false, fmt.Errorf("pipeline: repeat limit %d reached", x.MaxIterations))
}

func (r *Runner) runSwitch(ctx context.Context, x spec.SwitchRun, state *State) Outcome {
	for _, c := range x.Cases {
		ok, err := evalExpression(c.When, state, nil)
		if err != nil {
			return Failure("switch-expression", false, err)
		}
		if ok {
			return r.runCall(ctx, c.PipelineRef, mergeBindings(x.In, c.In), x.Out, state)
		}
	}
	if x.DefaultPipelineRef != "" {
		return r.runCall(ctx, x.DefaultPipelineRef, x.In, x.Out, state)
	}
	if x.NoMatch == "succeed" || x.NoMatch == "skip" {
		return Outcome{Class: OutcomeSkipped, Code: "no-match"}
	}
	return Failure("no-match", false, fmt.Errorf("pipeline: switch has no matching case"))
}

func (r *Runner) runFallback(ctx context.Context, x spec.FallbackRun, state *State) Outcome {
	var last Outcome
	limit := x.MaxAttempts
	if limit > len(x.Attempts) {
		limit = len(x.Attempts)
	}
	for i := 0; i < limit; i++ {
		a := x.Attempts[i]
		last = r.runCall(ctx, a.PipelineRef, mergeBindings(x.In, a.In), x.Out, state)
		if last.Class != OutcomeFailed {
			return last
		}
		if !contains(a.On, last.Class) && !contains(a.On, last.Code) {
			return last
		}
	}
	if x.NoMatch == "succeed" || x.NoMatch == "skip" {
		return Outcome{Class: OutcomeSkipped, Code: "fallback-exhausted"}
	}
	if last.Err == nil {
		last = Failure("fallback-exhausted", false, fmt.Errorf("pipeline: fallback exhausted %d attempts", limit))
	}
	return last
}

func (r *Runner) runForEach(ctx context.Context, x spec.ForEachRun, state *State) Outcome {
	items, _, _, ok := state.Read(x.Items.Field)
	if !ok {
		return Failure("missing-items", false, fmt.Errorf("pipeline: forEach reads missing %q", x.Items.Field))
	}
	rv := reflect.ValueOf(items)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return Failure("invalid-items", false, fmt.Errorf("pipeline: forEach items must be a list"))
	}
	if rv.Len() > x.MaxItems {
		return Failure("foreach-limit", false, fmt.Errorf("pipeline: forEach item limit %d exceeded", x.MaxItems))
	}
	results := make([]map[string]any, rv.Len())
	errs := make([]Outcome, rv.Len())
	sem := make(chan struct{}, x.MaxParallel)
	var wg sync.WaitGroup
	for i := 0; i < rv.Len(); i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs[i] = Failure("cancelled", false, ctx.Err())
				return
			}
			child := NewState()
			p := r.pipelines[x.PipelineRef]
			for port, path := range x.In {
				var v any
				if path == x.As {
					v = rv.Index(i).Interface()
				} else {
					v, _, _, _ = state.Read(path)
				}
				_ = child.SetTyped(port, p.Inputs[port], v)
			}
			o := r.runPipeline(ctx, x.PipelineRef, child)
			errs[i] = o
			results[i] = map[string]any{}
			for port := range x.Collect {
				v, _, _, _ := child.Read(port)
				results[i][port] = v
			}
		}()
	}
	wg.Wait()
	for _, o := range errs {
		if o.Class == OutcomeFailed {
			return o
		}
	}
	writes := map[string]any{}
	keys := make([]string, 0, len(x.Collect))
	for port := range x.Collect {
		keys = append(keys, port)
	}
	sort.Strings(keys)
	for _, port := range keys {
		values := make([]any, len(results))
		for i := range results {
			values[i] = results[i][port]
		}
		writes[x.Collect[port]] = values
	}
	if err := state.apply(writes); err != nil {
		return Failure("state-write", false, err)
	}
	return Outcome{Class: OutcomeSucceeded}
}

func contains(v []string, w string) bool {
	for _, x := range v {
		if x == w {
			return true
		}
	}
	return false
}
func cloneMap(v map[string]any) map[string]any {
	out := map[string]any{}
	for k, x := range v {
		out[k] = cloneValue(x)
	}
	return out
}
func cloneMapString(v map[string]string) map[string]string {
	out := map[string]string{}
	for k, x := range v {
		out[k] = x
	}
	return out
}
func mergeBindings(a, b map[string]string) map[string]string {
	out := cloneMapString(a)
	for k, v := range b {
		out[k] = v
	}
	return out
}
func backoff(b spec.Backoff, attempt int) time.Duration {
	n := float64(b.InitialMS)
	for i := 1; i < attempt; i++ {
		n *= b.Multiplier
	}
	if b.MaxMS > 0 && n > float64(b.MaxMS) {
		n = float64(b.MaxMS)
	}
	return time.Duration(n) * time.Millisecond
}
