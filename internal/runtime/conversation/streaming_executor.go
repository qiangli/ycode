package conversation

import (
	"context"
	"fmt"
	"sync"

	"github.com/qiangli/ycode/internal/tools"
)

// StreamingToolExecutor starts executing concurrency-safe tools as they
// stream in from the API, before all tool calls are fully received.
// Non-concurrent tools wait until the full response is received.
//
// Inspired by Claude Code's StreamingToolExecutor pattern.
type StreamingToolExecutor struct {
	registry *tools.Registry
	maxConc  int

	mu       sync.Mutex
	results  map[int]*streamResult
	wg       sync.WaitGroup
	sem      chan struct{} // bounded concurrency
	deferred []indexedCall // non-concurrent tools deferred until Wait()
}

type streamResult struct {
	output string
	err    error
}

type indexedCall struct {
	index int
	call  ToolCall
}

// NewStreamingToolExecutor creates a streaming tool executor.
func NewStreamingToolExecutor(registry *tools.Registry, maxConcurrent int) *StreamingToolExecutor {
	if maxConcurrent <= 0 {
		maxConcurrent = 8
	}
	return &StreamingToolExecutor{
		registry: registry,
		maxConc:  maxConcurrent,
		results:  make(map[int]*streamResult),
		sem:      make(chan struct{}, maxConcurrent),
	}
}

// Submit queues a tool call for execution. If the tool is concurrency-safe,
// execution starts immediately. Otherwise, it's deferred until Wait().
func (e *StreamingToolExecutor) Submit(ctx context.Context, idx int, call ToolCall) {
	spec, ok := e.registry.Get(call.Name)
	if !ok || !spec.IsConcurrencySafe {
		// Defer non-concurrent tools.
		e.mu.Lock()
		e.deferred = append(e.deferred, indexedCall{index: idx, call: call})
		e.mu.Unlock()
		return
	}

	// Start concurrent tool immediately.
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()

		// Acquire semaphore slot.
		e.sem <- struct{}{}
		defer func() { <-e.sem }()

		output, err := e.registry.Invoke(ctx, call.Name, call.Input)

		e.mu.Lock()
		e.results[idx] = &streamResult{output: output, err: err}
		e.mu.Unlock()
	}()
}

// Wait waits for all in-flight concurrent tools, then executes deferred
// (non-concurrent) tools serially. Returns results indexed by submission order.
func (e *StreamingToolExecutor) Wait(ctx context.Context) map[int]*streamResult {
	// Wait for concurrent tools.
	e.wg.Wait()

	// Execute deferred tools serially.
	e.mu.Lock()
	deferred := e.deferred
	e.deferred = nil
	e.mu.Unlock()

	for _, dc := range deferred {
		output, err := e.registry.Invoke(ctx, dc.call.Name, dc.call.Input)
		e.mu.Lock()
		e.results[dc.index] = &streamResult{output: output, err: err}
		e.mu.Unlock()
	}

	return e.results
}

// ResultCount returns the number of completed results.
func (e *StreamingToolExecutor) ResultCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.results)
}

// FormatResults formats streaming executor results into tool result content blocks.
func (e *StreamingToolExecutor) FormatResults(calls []ToolCall) []ContentBlockResult {
	results := e.results
	var blocks []ContentBlockResult

	for i, call := range calls {
		r, ok := results[i]
		block := ContentBlockResult{ToolUseID: call.ID}
		if !ok {
			block.Content = fmt.Sprintf("Error: tool %s did not produce a result", call.Name)
			block.IsError = true
		} else if r.err != nil {
			block.Content = fmt.Sprintf("Error: %v", r.err)
			block.IsError = true
		} else {
			block.Content = r.output
		}
		blocks = append(blocks, block)
	}

	return blocks
}

// ContentBlockResult holds a formatted tool result for the API.
type ContentBlockResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}
