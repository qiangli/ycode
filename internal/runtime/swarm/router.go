package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// WorkflowInfo describes an available workflow for routing.
type WorkflowInfo struct {
	Name        string
	Description string
}

// Router dispatches user messages to appropriate workflows.
type Router struct {
	// RouteFunc asks an LLM to pick the best workflow.
	// Receives the formatted prompt, returns the selected workflow name.
	RouteFunc func(prompt string) (string, error)
}

// NewRouter creates a router with the given LLM function.
func NewRouter(routeFunc func(string) (string, error)) *Router {
	return &Router{RouteFunc: routeFunc}
}

// Route selects the best workflow for a user message.
// Falls back to defaultWorkflow if the LLM can't decide.
func (r *Router) Route(message string, workflows []WorkflowInfo, defaultWorkflow string) (string, error) {
	tracer := otel.Tracer("ycode.swarm")
	_, span := tracer.Start(context.Background(), "ycode.swarm.route",
		trace.WithAttributes(
			attribute.String("router.message_preview", truncateForSpan(message, 100)),
			attribute.Int("router.workflow_count", len(workflows)),
			attribute.String("router.default", defaultWorkflow),
		))
	defer span.End()

	if r.RouteFunc == nil {
		return defaultWorkflow, nil
	}
	if len(workflows) == 0 {
		return defaultWorkflow, nil
	}

	prompt := FormatRoutingPrompt(message, workflows)
	result, err := r.RouteFunc(prompt)
	if err != nil {
		return defaultWorkflow, nil // silent fallback
	}

	// Validate the result is a known workflow.
	selected := strings.TrimSpace(result)
	for _, w := range workflows {
		if strings.EqualFold(w.Name, selected) {
			span.SetAttributes(
				attribute.String("router.selected", w.Name),
			)
			slog.Info("swarm.route",
				"selected", w.Name,
				"workflow_count", len(workflows),
				"default", defaultWorkflow,
			)
			return w.Name, nil
		}
	}

	span.SetAttributes(
		attribute.String("router.selected", defaultWorkflow),
	)
	slog.Info("swarm.route",
		"selected", defaultWorkflow,
		"workflow_count", len(workflows),
		"default", defaultWorkflow,
	)
	return defaultWorkflow, nil
}

// FormatRoutingPrompt creates the LLM prompt for workflow selection.
func FormatRoutingPrompt(message string, workflows []WorkflowInfo) string {
	var b strings.Builder
	b.WriteString("Given the user's message, select the most appropriate workflow.\n\n")
	b.WriteString("Available workflows:\n")
	for _, w := range workflows {
		fmt.Fprintf(&b, "- %s: %s\n", w.Name, w.Description)
	}
	fmt.Fprintf(&b, "\nUser message: %s\n\n", message)
	b.WriteString("Reply with ONLY the workflow name, nothing else.")
	return b.String()
}

func truncateForSpan(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
