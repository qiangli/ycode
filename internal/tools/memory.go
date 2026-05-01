package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qiangli/ycode/internal/runtime/memory"
)

// memManager is the module-level memory manager, set via SetMemoryManager.
var memManager *memory.Manager

// SetMemoryManager injects the memory manager for the memory tools.
func SetMemoryManager(m *memory.Manager) {
	memManager = m
}

// RegisterMemoryHandlers wires up the memory_save, memory_recall,
// memory_forget, memory_feedback, and memory_list tool handlers.
func RegisterMemoryHandlers(r *Registry) {
	if spec, ok := r.Get("memory_save"); ok {
		spec.Handler = handleMemorySave
	}
	if spec, ok := r.Get("memory_recall"); ok {
		spec.Handler = handleMemoryRecall
	}
	if spec, ok := r.Get("memory_forget"); ok {
		spec.Handler = handleMemoryForget
	}
	if spec, ok := r.Get("memory_feedback"); ok {
		spec.Handler = handleMemoryFeedback
	}
	if spec, ok := r.Get("memory_list"); ok {
		spec.Handler = handleMemoryList
	}
}

func checkMemoryManager() error {
	if memManager == nil {
		return fmt.Errorf("memory system is not initialized")
	}
	return nil
}

func handleMemorySave(_ context.Context, input json.RawMessage) (string, error) {
	if err := checkMemoryManager(); err != nil {
		return "", err
	}

	var params struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Content     string      `json:"content"`
		Type        memory.Type `json:"type"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse memory_save input: %w", err)
	}
	if params.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if params.Content == "" {
		return "", fmt.Errorf("content is required")
	}
	if params.Type == "" {
		params.Type = memory.TypeProject
	}

	mem := &memory.Memory{
		Name:        params.Name,
		Description: params.Description,
		Type:        params.Type,
		Content:     params.Content,
	}

	if err := memManager.Save(mem); err != nil {
		return "", err
	}

	return fmt.Sprintf("Memory %q saved (type: %s).", params.Name, params.Type), nil
}

func handleMemoryRecall(_ context.Context, input json.RawMessage) (string, error) {
	if err := checkMemoryManager(); err != nil {
		return "", err
	}

	var params struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse memory_recall input: %w", err)
	}
	if params.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if params.MaxResults <= 0 {
		params.MaxResults = 5
	}

	results, err := memManager.Recall(params.Query, params.MaxResults)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No matching memories found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d memory(ies):\n\n", len(results))
	for _, r := range results {
		fmt.Fprintf(&b, "---\n")
		fmt.Fprintf(&b, "**%s** (type: %s, score: %.2f)\n", r.Memory.Name, r.Memory.Type, r.Score)
		if r.Memory.Description != "" {
			fmt.Fprintf(&b, "_%s_\n", r.Memory.Description)
		}
		fmt.Fprintf(&b, "\n%s\n\n", r.Memory.Content)
	}
	return b.String(), nil
}

func handleMemoryForget(_ context.Context, input json.RawMessage) (string, error) {
	if err := checkMemoryManager(); err != nil {
		return "", err
	}

	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse memory_forget input: %w", err)
	}
	if params.Name == "" {
		return "", fmt.Errorf("name is required")
	}

	if err := memManager.Forget(params.Name); err != nil {
		return "", err
	}

	return fmt.Sprintf("Memory %q forgotten.", params.Name), nil
}

func handleMemoryFeedback(_ context.Context, input json.RawMessage) (string, error) {
	if err := checkMemoryManager(); err != nil {
		return "", err
	}

	var params struct {
		Name   string  `json:"name"`
		Reward float64 `json:"reward"` // 0.0-1.0
		Reason string  `json:"reason"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse memory_feedback input: %w", err)
	}
	if params.Name == "" {
		return "", fmt.Errorf("name is required")
	}

	all, err := memManager.All()
	if err != nil {
		return "", err
	}

	for _, mem := range all {
		if mem.Name == params.Name {
			memory.PropagateReward(mem, params.Reward, memory.DefaultRewardAlpha)
			if err := memManager.Save(mem); err != nil {
				return "", fmt.Errorf("save feedback: %w", err)
			}
			return fmt.Sprintf("Memory %q value updated to %.4f (reward: %.2f).", params.Name, mem.ValueScore, params.Reward), nil
		}
	}

	return "", fmt.Errorf("memory %q not found", params.Name)
}

func handleMemoryList(_ context.Context, input json.RawMessage) (string, error) {
	if err := checkMemoryManager(); err != nil {
		return "", err
	}

	var params struct {
		Type  memory.Type `json:"type"`
		Limit int         `json:"limit"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("parse memory_list input: %w", err)
	}
	if params.Limit <= 0 {
		params.Limit = 20
	}

	all, err := memManager.All()
	if err != nil {
		return "", err
	}

	// Filter by type if specified.
	var filtered []*memory.Memory
	for _, mem := range all {
		if params.Type != "" && mem.Type != params.Type {
			continue
		}
		filtered = append(filtered, mem)
		if len(filtered) >= params.Limit {
			break
		}
	}

	if len(filtered) == 0 {
		if params.Type != "" {
			return fmt.Sprintf("No memories found with type %q.", params.Type), nil
		}
		return "No memories found.", nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d memory(ies):\n\n", len(filtered))
	for _, mem := range filtered {
		fmt.Fprintf(&b, "- **%s** (type: %s)", mem.Name, mem.Type)
		if mem.Description != "" {
			fmt.Fprintf(&b, " — %s", mem.Description)
		}
		fmt.Fprintf(&b, "\n")
	}
	return b.String(), nil
}
