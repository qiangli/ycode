package agentdef

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
)

// Action is a function that takes input and produces output.
// Actions wrap agent spawns or tool invocations.
type Action func(ctx context.Context, input string) (string, error)

// FlowExecutor runs a list of actions composed according to a FlowType.
type FlowExecutor struct {
	flow        FlowType
	actions     []Action
	maxIter     int          // for FlowLoop; 0 means use default (10)
	dagWorkflow *DAGWorkflow // for FlowDAG
}

// NewFlowExecutor creates a flow executor for the given flow type and actions.
func NewFlowExecutor(flow FlowType, actions []Action) *FlowExecutor {
	return &FlowExecutor{
		flow:    flow,
		actions: actions,
		maxIter: 10,
	}
}

// SetDAGWorkflow sets the DAG workflow definition for FlowDAG execution.
func (fe *FlowExecutor) SetDAGWorkflow(w *DAGWorkflow) {
	fe.dagWorkflow = w
}

// SetMaxIterations sets the max iterations for loop flows.
func (fe *FlowExecutor) SetMaxIterations(n int) {
	if n > 0 {
		fe.maxIter = n
	}
}

// Run executes the flow with the given initial input.
func (fe *FlowExecutor) Run(ctx context.Context, input string) (string, error) {
	if len(fe.actions) == 0 {
		return input, nil
	}

	switch fe.flow {
	case FlowSequence, "":
		return fe.runSequence(ctx, input)
	case FlowChain:
		return fe.runChain(ctx, input)
	case FlowParallel:
		return fe.runParallel(ctx, input)
	case FlowLoop:
		return fe.runLoop(ctx, input)
	case FlowFallback:
		return fe.runFallback(ctx, input)
	case FlowChoice:
		return fe.runChoice(ctx, input)
	case FlowDAG:
		return fe.runDAG(ctx, input)
	default:
		return "", fmt.Errorf("unknown flow type: %s", fe.flow)
	}
}

// runSequence executes actions one after another, piping output to next input.
func (fe *FlowExecutor) runSequence(ctx context.Context, input string) (string, error) {
	current := input
	for _, action := range fe.actions {
		out, err := action(ctx, current)
		if err != nil {
			return "", err
		}
		current = out
	}
	return current, nil
}

// runChain executes actions as nested calls: A(B(C(input))).
// Innermost action runs first.
func (fe *FlowExecutor) runChain(ctx context.Context, input string) (string, error) {
	// Execute from last to first.
	current := input
	for i := len(fe.actions) - 1; i >= 0; i-- {
		out, err := fe.actions[i](ctx, current)
		if err != nil {
			return "", err
		}
		current = out
	}
	return current, nil
}

// runParallel executes all actions concurrently and combines results.
func (fe *FlowExecutor) runParallel(ctx context.Context, input string) (string, error) {
	type result struct {
		index  int
		output string
		err    error
	}

	results := make([]result, len(fe.actions))
	var wg sync.WaitGroup

	for i, action := range fe.actions {
		wg.Add(1)
		go func(idx int, act Action) {
			defer wg.Done()
			out, err := act(ctx, input)
			results[idx] = result{index: idx, output: out, err: err}
		}(i, action)
	}

	wg.Wait()

	// Combine results in order; fail if any action failed.
	var combined string
	for _, r := range results {
		if r.err != nil {
			return "", fmt.Errorf("parallel action %d: %w", r.index, r.err)
		}
		if combined != "" {
			combined += "\n---\n"
		}
		combined += r.output
	}
	return combined, nil
}

// runLoop repeats the first action until max iterations or context cancellation.
func (fe *FlowExecutor) runLoop(ctx context.Context, input string) (string, error) {
	action := fe.actions[0]
	current := input

	for i := 0; i < fe.maxIter; i++ {
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		default:
		}

		out, err := action(ctx, current)
		if err != nil {
			return current, err
		}
		current = out
	}
	return current, nil
}

// runFallback tries actions in order, returning the first success.
func (fe *FlowExecutor) runFallback(ctx context.Context, input string) (string, error) {
	var lastErr error
	for _, action := range fe.actions {
		out, err := action(ctx, input)
		if err == nil {
			return out, nil
		}
		lastErr = err
	}
	return "", fmt.Errorf("all fallback actions failed, last: %w", lastErr)
}

// runChoice randomly selects and executes one action.
func (fe *FlowExecutor) runChoice(ctx context.Context, input string) (string, error) {
	idx := rand.IntN(len(fe.actions))
	return fe.actions[idx](ctx, input)
}

// runDAG executes a DAG workflow using the first action as the node handler.
// The input is passed as the initial context. Node outputs are collected.
func (fe *FlowExecutor) runDAG(ctx context.Context, input string) (string, error) {
	if fe.dagWorkflow == nil {
		return "", fmt.Errorf("FlowDAG requires a DAGWorkflow to be set")
	}
	handler := func(ctx context.Context, node DAGNode, vars map[string]string) (string, error) {
		// Substitute variables in the node prompt/command.
		prompt := SubstituteVariables(node.Prompt, vars)
		if prompt == "" {
			prompt = SubstituteVariables(node.Command, vars)
		}
		if prompt == "" {
			prompt = input
		}
		// Use the first action as the executor.
		if len(fe.actions) > 0 {
			return fe.actions[0](ctx, prompt)
		}
		return prompt, nil
	}
	executor := NewDAGExecutor(handler)
	outputs, err := executor.Run(ctx, fe.dagWorkflow)
	if err != nil {
		return "", err
	}
	// Return the last node's output.
	if len(fe.dagWorkflow.Nodes) > 0 {
		lastNode := fe.dagWorkflow.Nodes[len(fe.dagWorkflow.Nodes)-1]
		if out, ok := outputs[lastNode.ID]; ok {
			return out, nil
		}
	}
	return input, nil
}
