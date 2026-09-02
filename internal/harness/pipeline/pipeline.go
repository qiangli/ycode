// Package pipeline executes the ordered stages compiled from agent.yaml.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/qiangli/coreutils/pkg/dag"

	"github.com/qiangli/ycode/internal/harness/spec"
)

// State is the typed pipeline carrier. Named slots keep stages independent;
// registration declares which concrete values each slot contains.
type State struct {
	mu     sync.RWMutex
	values map[string]any
}

func NewState() *State { return &State{values: make(map[string]any)} }

func (s *State) Set(name string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[name] = value
}

func (s *State) Get(name string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.values[name]
	return v, ok
}

func (s *State) clone() *State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make(map[string]any, len(s.values))
	for key, value := range s.values {
		values[key] = value
	}
	return &State{values: values}
}

type Handler func(context.Context, *State, map[string]any) error
type Predicate func(*State) bool

type Registry struct {
	handlers   map[string]Handler
	predicates map[string]Predicate
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler), predicates: make(map[string]Predicate)}
}

func (r *Registry) RegisterStage(name string, handler Handler) error {
	if name == "" || handler == nil {
		return errors.New("pipeline: stage name and handler are required")
	}
	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("pipeline: stage %q already registered", name)
	}
	r.handlers[name] = handler
	return nil
}

func (r *Registry) RegisterPredicate(name string, predicate Predicate) error {
	if name == "" || predicate == nil {
		return errors.New("pipeline: predicate name and implementation are required")
	}
	if _, exists := r.predicates[name]; exists {
		return fmt.Errorf("pipeline: predicate %q already registered", name)
	}
	r.predicates[name] = predicate
	return nil
}

type Runner struct {
	registry  *Registry
	pipelines map[string]spec.Pipeline
}

func NewRunner(registry *Registry) *Runner { return &Runner{registry: registry} }

// WithPipelines supplies the catalog used by explicit call stages.
func (r *Runner) WithPipelines(pipelines map[string]spec.Pipeline) *Runner {
	r.pipelines = pipelines
	return r
}

// RunPipeline executes a named graph through the shared Bashy DAG scheduler.
// Dependencies, fan-out, fan-in, fail-fast and concurrency come exclusively
// from the compiled Pipeline document.
func (r *Runner) RunPipeline(ctx context.Context, name string, state *State) error {
	p, ok := r.pipelines[name]
	if !ok {
		return fmt.Errorf("pipeline %q is not registered", name)
	}
	document := &dag.Document{}
	nodes := make(map[string]spec.Stage, len(p.Nodes))
	for _, stage := range p.Nodes {
		nodes[stage.ID] = stage
		document.Tasks = append(document.Tasks, &dag.Task{Name: stage.ID, Body: stage.ID, Requires: stage.Needs})
		document.Order = append(document.Order, stage.ID)
	}
	graph, err := dag.BuildGraph(document)
	if err != nil {
		return err
	}
	engine := dag.Engine{
		Graph:       graph,
		Concurrency: p.Concurrency,
		FailFast:    p.FailFast,
		Capture:     true,
		Executor:    &stageExecutor{runner: r, nodes: nodes, state: state},
		Stdout:      io.Discard,
		Stderr:      io.Discard,
	}
	report, err := engine.Run(ctx)
	if err != nil {
		return err
	}
	if report.Failed {
		var failures []error
		for _, result := range report.Results {
			if result.Status == dag.StatusFailed {
				failures = append(failures, fmt.Errorf("node %q: %w", result.Name, result.Err))
			}
		}
		return errors.Join(failures...)
	}
	return nil
}

type stageExecutor struct {
	runner *Runner
	nodes  map[string]spec.Stage
	state  *State
}

func (e *stageExecutor) Execute(ctx context.Context, task *dag.Task, _ dag.TaskIO) dag.TaskResult {
	started := time.Now()
	result := dag.TaskResult{Name: task.Name, Status: dag.StatusDone}
	if err := e.runner.run(ctx, []spec.Stage{e.nodes[task.Name]}, e.state); err != nil {
		result.Status = dag.StatusFailed
		result.ExitCode = 1
		result.Err = err
	}
	result.Duration = time.Since(started)
	return result
}

func (r *Runner) Run(ctx context.Context, stages []spec.Stage, state *State) error {
	if r == nil || r.registry == nil {
		return errors.New("pipeline: registry is required")
	}
	if state == nil {
		return errors.New("pipeline: state is required")
	}
	return r.run(ctx, stages, state)
}

func (r *Runner) run(ctx context.Context, stages []spec.Stage, state *State) error {
	for _, stage := range stages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if stage.Use != "" {
			handler, ok := r.registry.handlers[stage.Use]
			if !ok {
				return fmt.Errorf("pipeline stage %q: type %q is not registered", stage.ID, stage.Use)
			}
			if err := handler(ctx, state, stage.With); err != nil {
				return fmt.Errorf("pipeline stage %q: %w", stage.ID, err)
			}
			continue
		}
		if stage.Call != "" {
			called, ok := r.pipelines[stage.Call]
			if !ok {
				return fmt.Errorf("pipeline stage %q: pipeline %q is not registered", stage.ID, stage.Call)
			}
			if err := r.run(ctx, called.Nodes, state); err != nil {
				return fmt.Errorf("pipeline stage %q: %w", stage.ID, err)
			}
			continue
		}
		if stage.When != "" {
			predicate, ok := r.registry.predicates[stage.When]
			if !ok {
				return fmt.Errorf("pipeline stage %q: predicate %q is not registered", stage.ID, stage.When)
			}
			if predicate(state) {
				if err := r.run(ctx, stage.Stages, state); err != nil {
					return err
				}
			}
			continue
		}
		if stage.Repeat != nil {
			predicate, ok := r.registry.predicates[stage.Repeat.Until]
			if !ok {
				return fmt.Errorf("pipeline stage %q: predicate %q is not registered", stage.ID, stage.Repeat.Until)
			}
			complete := false
			for range stage.Repeat.Max {
				if err := r.run(ctx, stage.Repeat.Stages, state); err != nil {
					return err
				}
				if predicate(state) {
					complete = true
					break
				}
			}
			if !complete {
				return fmt.Errorf("pipeline stage %q: repeat limit %d reached", stage.ID, stage.Repeat.Max)
			}
			continue
		}
		if stage.Retry != nil {
			var last error
			for range stage.Retry.Max {
				if err := r.run(ctx, stage.Retry.Stages, state); err == nil {
					last = nil
					break
				} else {
					last = err
				}
			}
			if last != nil {
				return fmt.Errorf("pipeline stage %q: retry limit %d reached: %w", stage.ID, stage.Retry.Max, last)
			}
			continue
		}
		if stage.Switch != nil {
			selected := stage.Switch.Default
			for _, item := range stage.Switch.Cases {
				predicate, ok := r.registry.predicates[item.When]
				if !ok {
					return fmt.Errorf("pipeline stage %q: predicate %q is not registered", stage.ID, item.When)
				}
				if predicate(state) {
					selected = item.Stages
					break
				}
			}
			if err := r.run(ctx, selected, state); err != nil {
				return err
			}
			continue
		}
		if stage.ForEach != nil {
			if err := r.runForEach(ctx, stage, state); err != nil {
				return err
			}
			continue
		}
		if len(stage.Fallback) > 0 {
			var errs []error
			for _, alternative := range stage.Fallback {
				if err := r.run(ctx, []spec.Stage{alternative}, state); err == nil {
					errs = nil
					break
				} else {
					errs = append(errs, err)
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("pipeline stage %q: all fallbacks failed: %w", stage.ID, errors.Join(errs...))
			}
		}
	}
	return nil
}

func (r *Runner) runForEach(ctx context.Context, stage spec.Stage, state *State) error {
	raw, ok := state.Get(stage.ForEach.Items)
	if !ok {
		return fmt.Errorf("pipeline stage %q: state %q is unavailable", stage.ID, stage.ForEach.Items)
	}
	value := reflect.ValueOf(raw)
	if value.Kind() != reflect.Array && value.Kind() != reflect.Slice {
		return fmt.Errorf("pipeline stage %q: state %q is not a collection", stage.ID, stage.ForEach.Items)
	}
	type indexedResult struct {
		index int
		value any
		err   error
	}
	results := make(chan indexedResult, value.Len())
	semaphore := make(chan struct{}, stage.ForEach.MaxParallel)
	var group sync.WaitGroup
	for index := range value.Len() {
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- indexedResult{index: index, err: ctx.Err()}
				return
			}
			child := state.clone()
			child.Set(stage.ForEach.As, value.Index(index).Interface())
			err := r.run(ctx, stage.ForEach.Stages, child)
			collected, _ := child.Get(stage.ForEach.Collect)
			results <- indexedResult{index: index, value: collected, err: err}
		}()
	}
	group.Wait()
	close(results)
	collected := make([]any, 0, value.Len())
	if stage.ForEach.Ordered {
		collected = make([]any, value.Len())
	}
	var errs []error
	for result := range results {
		if result.err != nil {
			errs = append(errs, fmt.Errorf("item %d: %w", result.index, result.err))
		}
		if stage.ForEach.Ordered {
			collected[result.index] = result.value
		} else {
			collected = append(collected, result.value)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("pipeline stage %q: %w", stage.ID, errors.Join(errs...))
	}
	state.Set(stage.ForEach.Collect, collected)
	return nil
}
