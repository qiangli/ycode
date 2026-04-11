// Package taskqueue provides bounded-parallelism execution for tool calls.
//
// It uses semaphore channels to enforce per-category concurrency limits:
// standard tools (file ops, search) share one pool, LLM tools (agents)
// share another. Results are returned in the original call order.
package taskqueue

import (
	"context"
	"sync"
)

// TaskStatus represents the lifecycle of a queued tool execution.
type TaskStatus int

const (
	StatusQueued TaskStatus = iota
	StatusRunning
	StatusCompleted
	StatusFailed
)

// String returns a human-readable status label.
func (s TaskStatus) String() string {
	switch s {
	case StatusQueued:
		return "queued"
	case StatusRunning:
		return "running"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// TaskEvent is sent over the progress channel whenever a task changes state.
type TaskEvent struct {
	Index  int    // position in the original call slice
	Name   string // tool name
	Detail string // human-readable label (e.g. "Read(config.go)")
	Status TaskStatus
	Total  int // total number of tasks in this batch
}

// TaskResult holds the outcome of a single tool execution.
type TaskResult struct {
	Index  int
	Output string
	Err    error
}

// Category classifies a tool for concurrency scheduling.
// This mirrors tools.ToolCategory but avoids a circular import.
type Category int

const (
	CatStandard    Category = iota // file ops, search, web
	CatLLM                         // Agent, TaskCreate
	CatInteractive                 // AskUserQuestion
)

// Call describes a single tool invocation to be executed by the queue.
type Call struct {
	Index    int
	Name     string
	Detail   string
	Category Category
	Invoke   func(ctx context.Context) (string, error)
}

// Executor runs tool calls with bounded parallelism per category.
type Executor struct {
	maxStandard int
	maxLLM      int
}

// NewExecutor creates an executor with the given concurrency limits.
// If maxStandard or maxLLM are <= 0, they default to 8 and 2 respectively.
func NewExecutor(maxStandard, maxLLM int) *Executor {
	if maxStandard <= 0 {
		maxStandard = 8
	}
	if maxLLM <= 0 {
		maxLLM = 2
	}
	return &Executor{
		maxStandard: maxStandard,
		maxLLM:      maxLLM,
	}
}

// Run executes all calls concurrently (within limits) and returns results
// in the same order as the input calls.
//
// Progress events are sent to the progress channel if non-nil. The caller
// is responsible for closing the channel after Run returns.
//
// Failures in individual calls do not cancel siblings. Each result captures
// its own error independently. Context cancellation stops queued tasks from
// starting and propagates to in-flight invocations.
func (e *Executor) Run(ctx context.Context, calls []Call, progress chan<- TaskEvent) []TaskResult {
	n := len(calls)
	if n == 0 {
		return nil
	}

	results := make([]TaskResult, n)
	stdSem := make(chan struct{}, e.maxStandard)
	llmSem := make(chan struct{}, e.maxLLM)

	// Interactive tools serialize through a mutex (at most one user prompt at a time)
	// while also consuming a standard semaphore slot.
	var interactiveMu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(n)

	for _, call := range calls {
		emit := func(status TaskStatus) {
			if progress != nil {
				progress <- TaskEvent{
					Index:  call.Index,
					Name:   call.Name,
					Detail: call.Detail,
					Status: status,
					Total:  n,
				}
			}
		}

		go func() {
			defer wg.Done()

			emit(StatusQueued)

			// Check for cancellation before acquiring semaphore.
			if ctx.Err() != nil {
				results[call.Index] = TaskResult{Index: call.Index, Err: ctx.Err()}
				emit(StatusFailed)
				return
			}

			// Acquire the appropriate semaphore.
			sem := stdSem
			if call.Category == CatLLM {
				sem = llmSem
			}

			select {
			case sem <- struct{}{}:
				// Acquired.
			case <-ctx.Done():
				results[call.Index] = TaskResult{Index: call.Index, Err: ctx.Err()}
				emit(StatusFailed)
				return
			}
			defer func() { <-sem }()

			// Interactive tools additionally serialize.
			if call.Category == CatInteractive {
				interactiveMu.Lock()
				defer interactiveMu.Unlock()
			}

			emit(StatusRunning)

			output, err := call.Invoke(ctx)
			results[call.Index] = TaskResult{
				Index:  call.Index,
				Output: output,
				Err:    err,
			}

			if err != nil {
				emit(StatusFailed)
			} else {
				emit(StatusCompleted)
			}
		}()
	}

	wg.Wait()
	return results
}
