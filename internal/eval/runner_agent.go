package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/qiangli/ycode/internal/api"
	"github.com/qiangli/ycode/internal/harness/spec"
	"github.com/qiangli/ycode/pkg/ycode"
)

// AgentRunner evaluates the same compiled harness used by production
// entrypoints. The provider argument replaces only the configured transport;
// model selection, tools, policy and graph behavior remain YAML-owned.
func AgentRunner(cfg RunConfig, provider api.Provider) *Runner {
	return NewRunner(cfg, func(ctx context.Context, scenario *Scenario) (*RunResult, error) {
		return executeWithHarness(ctx, scenario, cfg, provider)
	})
}

func executeWithHarness(ctx context.Context, scenario *Scenario, cfg RunConfig, backend api.Provider) (*RunResult, error) {
	workspace, err := os.MkdirTemp("", "ycode-eval-*")
	if err != nil {
		return &RunResult{}, err
	}
	defer os.RemoveAll(workspace)
	var cleanup func()
	if scenario.Setup != nil {
		cleanup, err = scenario.Setup(workspace)
		if err != nil {
			return &RunResult{WorkDir: workspace}, err
		}
	}
	if cleanup != nil {
		defer cleanup()
	}

	source := cfg.HarnessFile
	if source == "" {
		source = os.Getenv("YCODE_AGENT_FILE")
	}
	if source == "" {
		return &RunResult{WorkDir: workspace}, errors.New("eval requires RunConfig.HarnessFile or YCODE_AGENT_FILE")
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return &RunResult{WorkDir: workspace}, err
	}
	path := filepath.Join(workspace, "agent.yaml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return &RunResult{WorkDir: workspace}, err
	}
	doc, err := spec.Load(path)
	if err != nil {
		return &RunResult{WorkDir: workspace}, err
	}
	providerRef, err := defaultProviderRef(doc)
	if err != nil {
		return &RunResult{WorkDir: workspace}, err
	}
	harness, err := ycode.Load(path, ycode.WithHarnessProvider(providerRef, backend))
	if err != nil {
		return &RunResult{WorkDir: workspace}, err
	}
	defer harness.Close()
	body, _ := json.Marshal(map[string]string{"request": scenario.Prompt})
	runID := uuid.NewString()
	started := time.Now()
	stream, err := harness.Run(ctx, ycode.RunRequest{SessionID: uuid.NewString(), RunID: runID, TriggerRef: "interactive-input", FrontendRef: "embed", Principal: "eval", IdempotencyKey: runID, Body: body})
	if err != nil {
		return &RunResult{WorkDir: workspace, Duration: time.Since(started)}, err
	}
	result := &RunResult{WorkDir: workspace}
	for item := range stream {
		switch item.Type {
		case "llm.requested":
			result.Turns++
		case "bashy.intent.compiled":
			result.ToolCalls = append(result.ToolCalls, ToolCall{Name: "bashy", Input: append(json.RawMessage(nil), item.Data...)})
		case "output.emitted":
			var data struct {
				Deliveries []struct {
					PayloadRef string `json:"payload_ref"`
				} `json:"deliveries"`
			}
			if json.Unmarshal(item.Data, &data) == nil && len(data.Deliveries) == 1 {
				payload, payloadErr := harness.Payload(data.Deliveries[0].PayloadRef)
				if payloadErr != nil {
					result.Error = payloadErr
				} else {
					result.Response = string(payload)
				}
			}
		case "turn.failed":
			result.Error = errors.New("harness turn failed")
		}
	}
	result.Duration = time.Since(started)
	return result, nil
}

func defaultProviderRef(doc *spec.Document) (string, error) {
	agent := doc.Spec.Agents[doc.Spec.Runtime.DefaultAgentRef]
	route := doc.Spec.Routes[agent.ModelRouteRef]
	if len(route.Attempts) == 0 {
		return "", errors.New("default agent route has no attempts")
	}
	model, ok := doc.Spec.Models[route.Attempts[0].ModelRef]
	if !ok || model.ProviderRef == "" {
		return "", fmt.Errorf("default route model %q has no provider", route.Attempts[0].ModelRef)
	}
	return model.ProviderRef, nil
}
