package pipeline

import (
	"context"
	"fmt"
	"sync"
)

const (
	OutcomeSucceeded = "succeeded"
	OutcomeObserved  = "observed"
	OutcomeFailed    = "failed"
	OutcomeSkipped   = "skipped"
	OutcomeCancelled = "cancelled"
)

type Outcome struct {
	Class     string
	Code      string
	Retryable bool
	Outputs   map[string]any
	Err       error
}

func Success(outputs map[string]any) Outcome {
	return Outcome{Class: OutcomeSucceeded, Outputs: outputs}
}
func Failure(code string, retryable bool, err error) Outcome {
	return Outcome{Class: OutcomeFailed, Code: code, Retryable: retryable, Err: err}
}

type Invocation struct {
	StageID string
	Inputs  map[string]any
	With    map[string]any
}
type Handler func(context.Context, Invocation) Outcome
type Definition struct {
	Name            string
	Inputs, Outputs map[string]string
	Handler         Handler
}

type Registry struct {
	mu     sync.RWMutex
	stages map[string]Definition
}

func NewRegistry() *Registry { return &Registry{stages: make(map[string]Definition)} }
func (r *Registry) Register(def Definition) error {
	if def.Name == "" || def.Handler == nil {
		return fmt.Errorf("pipeline: stage definition requires name and handler")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.stages[def.Name]; ok {
		return fmt.Errorf("pipeline: stage %q already registered", def.Name)
	}
	r.stages[def.Name] = def
	return nil
}
func (r *Registry) lookup(name string) (Definition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.stages[name]
	if !ok {
		return Definition{}, fmt.Errorf("pipeline: stage %q is not registered", name)
	}
	return d, nil
}
