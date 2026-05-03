package agentdef

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// NodeHandler executes a single DAG node and returns its output.
type NodeHandler func(ctx context.Context, node DAGNode, variables map[string]string) (string, error)

// DAGExecutor runs a DAG workflow with concurrent layer execution.
type DAGExecutor struct {
	handler NodeHandler

	// WorktreeSetup is called before DAG execution to create an isolated worktree.
	// Returns the worktree path and a cleanup function.
	WorktreeSetup func(workflowName string) (worktreePath string, cleanup func(), err error)
}

// NewDAGExecutor creates a DAG executor with the given node handler.
func NewDAGExecutor(handler NodeHandler) *DAGExecutor {
	return &DAGExecutor{handler: handler}
}

// Run executes the DAG workflow. Returns the outputs of all nodes.
func (de *DAGExecutor) Run(ctx context.Context, workflow *DAGWorkflow) (map[string]string, error) {
	if de.WorktreeSetup != nil {
		wtPath, cleanup, err := de.WorktreeSetup(workflow.Name)
		if err != nil {
			return nil, fmt.Errorf("worktree setup: %w", err)
		}
		if cleanup != nil {
			defer cleanup()
		}
		_ = wtPath // available for node handlers via closure
	}

	layers, err := TopologicalSort(workflow.Nodes)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	tracer := otel.Tracer("ycode.dag")
	ctx, span := tracer.Start(ctx, "ycode.dag.run",
		trace.WithAttributes(
			attribute.String("dag.workflow", workflow.Name),
			attribute.Int("dag.node_count", len(workflow.Nodes)),
			attribute.Int("dag.layer_count", len(layers)),
		))

	outputs := make(map[string]string)
	defer func() {
		span.SetAttributes(attribute.Int("dag.outputs_count", len(outputs)))
		span.End()
	}()
	var mu sync.Mutex

	for layerIdx, layer := range layers {
		_, layerSpan := tracer.Start(ctx, "ycode.dag.layer",
			trace.WithAttributes(
				attribute.Int("dag.layer_index", layerIdx),
				attribute.Int("dag.layer_size", len(layer)),
			))

		if len(layer) == 1 {
			// Single node — run directly.
			node := layer[0]

			// Evaluate condition if present.
			if skip, _ := shouldSkipNode(ctx, node, outputs); skip {
				slog.Info("dag.node.skipped", "workflow", workflow.Name, "node", node.ID, "reason", "condition false")
			} else {
				slog.Info("dag.node.execute", "workflow", workflow.Name, "node", node.ID, "type", string(node.Type), "layer", layerIdx)
				var out string
				if node.FanOut != nil {
					mu.Lock()
					vars := make(map[string]string, len(outputs))
					for k, v := range outputs {
						vars[k] = v
					}
					mu.Unlock()
					out, err = de.executeFanOut(ctx, node, vars)
				} else {
					nodeCtx := injectNodeRestrictions(ctx, node)
					out, err = de.handler(nodeCtx, node, outputs)
				}
				if err != nil {
					return outputs, fmt.Errorf("node %s: %w", node.ID, err)
				}
				mu.Lock()
				outputs[node.ID] = out
				mu.Unlock()
			}
		} else {
			// Multiple nodes — run concurrently.
			var wg sync.WaitGroup
			errs := make([]error, len(layer))

			for i, node := range layer {
				wg.Add(1)
				go func(idx int, n DAGNode) {
					defer wg.Done()

					// Snapshot outputs for variable substitution and condition evaluation.
					mu.Lock()
					vars := make(map[string]string, len(outputs))
					for k, v := range outputs {
						vars[k] = v
					}
					mu.Unlock()

					// Evaluate condition if present.
					if skip, _ := shouldSkipNode(ctx, n, vars); skip {
						slog.Info("dag.node.skipped", "workflow", workflow.Name, "node", n.ID, "reason", "condition false")
						return
					}

					slog.Info("dag.node.execute", "workflow", workflow.Name, "node", n.ID, "type", string(n.Type), "layer", layerIdx)

					var out string
					var err error
					if n.FanOut != nil {
						out, err = de.executeFanOut(ctx, n, vars)
					} else {
						nodeCtx := injectNodeRestrictions(ctx, n)
						out, err = de.handler(nodeCtx, n, vars)
					}
					if err != nil {
						errs[idx] = fmt.Errorf("node %s: %w", n.ID, err)
						return
					}
					mu.Lock()
					outputs[n.ID] = out
					mu.Unlock()
				}(i, node)
			}
			wg.Wait()

			// Check for errors.
			for _, err := range errs {
				if err != nil {
					layerSpan.End()
					return outputs, err
				}
			}
		}
		layerSpan.End()
	}

	return outputs, nil
}

// shouldSkipNode evaluates the node's When condition and returns true if the node should be skipped.
func shouldSkipNode(ctx context.Context, node DAGNode, vars map[string]string) (bool, error) {
	if node.When == nil {
		return false, nil
	}
	cond, err := node.When.Build()
	if err != nil {
		return false, fmt.Errorf("build condition for node %q: %w", node.ID, err)
	}
	ok, err := cond.Evaluate(ctx, vars)
	if err != nil {
		return false, fmt.Errorf("evaluate condition for node %q: %w", node.ID, err)
	}
	return !ok, nil // skip if condition is false
}

// executeFanOut splits the source node's output and executes the node once per item.
// Results are collected and joined with the configured delimiter.
// Inspired by LangGraph's Send() pattern for dynamic map-reduce parallelism.
func (de *DAGExecutor) executeFanOut(ctx context.Context, node DAGNode, outputs map[string]string) (string, error) {
	cfg := node.FanOut
	sourceOutput, ok := outputs[cfg.SourceNode]
	if !ok {
		return "", fmt.Errorf("fan_out source node %q has no output", cfg.SourceNode)
	}

	items := strings.Split(sourceOutput, cfg.EffectiveSplitOn())
	// Filter empty items.
	var nonEmpty []string
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			nonEmpty = append(nonEmpty, item)
		}
	}
	items = nonEmpty

	if len(items) == 0 {
		return "", nil
	}

	slog.Info("dag.fan_out", "node", node.ID, "source", cfg.SourceNode, "items", len(items))

	maxP := cfg.MaxParallel
	if maxP <= 0 {
		maxP = len(items)
	}

	results := make([]string, len(items))
	errs := make([]error, len(items))
	sem := make(chan struct{}, maxP)
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, fanItem string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Build per-item variables with $fan.item and $fan.index.
			vars := make(map[string]string, len(outputs)+2)
			for k, v := range outputs {
				vars[k] = v
			}
			vars["fan.item"] = fanItem
			vars["fan.index"] = fmt.Sprintf("%d", idx)

			// Create a copy of the node with substituted prompt/command.
			fanNode := node
			fanNode.Prompt = SubstituteVariables(fanNode.Prompt, vars)
			fanNode.Command = SubstituteVariables(fanNode.Command, vars)

			nodeCtx := injectNodeRestrictions(ctx, fanNode)
			out, err := de.handler(nodeCtx, fanNode, vars)
			if err != nil {
				errs[idx] = fmt.Errorf("fan_out item %d: %w", idx, err)
				return
			}
			results[idx] = out
		}(i, item)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return "", err
		}
	}

	return strings.Join(results, cfg.EffectiveJoinWith()), nil
}

// injectNodeRestrictions adds per-node tool restrictions to the context if configured.
func injectNodeRestrictions(ctx context.Context, node DAGNode) context.Context {
	if len(node.AllowedTools) == 0 && len(node.DeniedTools) == 0 {
		return ctx
	}
	return ContextWithNodeRestrictions(ctx, NodeToolRestrictions{
		AllowedTools: node.AllowedTools,
		DeniedTools:  node.DeniedTools,
	})
}
