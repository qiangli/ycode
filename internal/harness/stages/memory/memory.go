// Package memory implements explicit YAML harness context measurement and
// compaction stages. Recall and persist are not Go mechanisms: with the
// bashy-kb provider they are harness-authored bashy.run nodes over `bashy kb`,
// compiled from the same YAML memory policy this engine reads.
package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/qiangli/ycode/internal/harness/event"
	"github.com/qiangli/ycode/internal/harness/message"
	"github.com/qiangli/ycode/internal/harness/spec"
)

const SchemaVersion = "ycode.memory-stage/v1"

type TokenCounter interface {
	CountMessages([]message.Message) (int, error)
}

type Summarizer interface {
	Summarize(context.Context, string, string, []message.Message, string) (string, error)
}

type Config struct {
	Document *spec.Document
	Events   *event.Store
	Payloads *event.PayloadStore
	Tokens   TokenCounter
	Summary  Summarizer
}

type Engine struct {
	memories map[string]spec.Memory
	routes   map[string]spec.Route
	models   map[string]spec.Model
	sources  map[string]spec.Source
	events   *event.Store
	payloads *event.PayloadStore
	tokens   TokenCounter
	summary  Summarizer
}

func New(config Config) (*Engine, error) {
	if config.Document == nil || config.Events == nil || config.Payloads == nil || config.Tokens == nil {
		return nil, errors.New("memory stages require compiled document, stores and token counter")
	}
	return &Engine{memories: cloneMemories(config.Document.Spec.Memories), routes: cloneRoutes(config.Document.Spec.Routes), models: cloneModels(config.Document.Spec.Models), sources: cloneSources(config.Document.Spec.Sources), events: config.Events, payloads: config.Payloads, tokens: config.Tokens, summary: config.Summary}, nil
}

func (e *Engine) BindSummary(summary Summarizer) {
	e.summary = summary
}

type Meta struct {
	SessionID    string
	RunID        string
	StageID      string
	ConfigDigest string
}

func (m Meta) validate() error {
	if m.SessionID == "" || m.RunID == "" || m.StageID == "" || m.ConfigDigest == "" {
		return errors.New("memory stage requires session, run, stage and config digest")
	}
	return nil
}

type Measurement struct {
	SchemaVersion  string                `json:"schema_version"`
	PayloadRef     string                `json:"payload_ref"`
	Tokens         int                   `json:"tokens"`
	ContextBudget  int                   `json:"context_budget"`
	TruncateBudget int                   `json:"truncate_budget"`
	Measured       MeasurementProvenance `json:"measured"`
}

type MeasurementProvenance struct {
	Source         string  `json:"source"`
	ProviderTokens int     `json:"provider_tokens"`
	EstimatedTail  int     `json:"estimated_tail"`
	Margin         float64 `json:"margin"`
}

type MeasureRequest struct {
	MemoryRef    string
	RouteRef     string
	SafetyMargin float64
	Messages     []message.Message
}

func (e *Engine) Measure(_ context.Context, meta Meta, request MeasureRequest) (Measurement, error) {
	if err := meta.validate(); err != nil {
		return Measurement{}, err
	}
	if request.SafetyMargin <= 0 {
		return Measurement{}, errors.New("context measure: safety margin must be positive")
	}
	memoryConfig, err := e.memory(request.MemoryRef)
	if err != nil {
		return Measurement{}, err
	}
	route, ok := e.routes[request.RouteRef]
	if !ok || len(route.Attempts) == 0 {
		return Measurement{}, fmt.Errorf("context measure: undeclared route %q", request.RouteRef)
	}
	model, ok := e.models[route.Attempts[0].ModelRef]
	if !ok {
		return Measurement{}, fmt.Errorf("context measure: undeclared model %q", route.Attempts[0].ModelRef)
	}
	encoded, err := json.Marshal(request.Messages)
	if err != nil {
		return Measurement{}, err
	}
	ref, err := e.payloads.Put(encoded)
	if err != nil {
		return Measurement{}, err
	}
	count, measured, err := e.measureMessages(request.Messages, request.SafetyMargin)
	if err != nil {
		return Measurement{}, errors.New("context measure: token counter failed")
	}
	// Reserve room for one response exactly as the request does
	// (turn/routetext.go): the route budget capped at the model's output limit.
	// The route budget can be a whole-run allowance far above one response.
	outputReserve := route.Budget.MaxOutputTokens
	if outputReserve > model.Limits.MaxOutputTokens {
		outputReserve = model.Limits.MaxOutputTokens
	}
	result := Measurement{SchemaVersion: SchemaVersion, PayloadRef: ref, Tokens: count, ContextBudget: model.Limits.ContextTokens - outputReserve - memoryConfig.Compaction.ReserveTokens, Measured: measured}
	if result.ContextBudget < 0 {
		return Measurement{}, errors.New("context measure: computed context budget is negative")
	}
	result.TruncateBudget = result.ContextBudget - memoryConfig.Compaction.ReserveTokens
	if result.TruncateBudget < 0 {
		result.TruncateBudget = 0
	}
	_, err = e.append(meta, "context.measured", result)
	return result, err
}

func (e *Engine) measureMessages(messages []message.Message, margin float64) (int, MeasurementProvenance, error) {
	lastUsage := -1
	providerTokens := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == message.RoleAssistant && messages[i].Usage != nil {
			lastUsage = i
			providerTokens = usageTotal(*messages[i].Usage)
			break
		}
	}
	if lastUsage < 0 {
		estimated, err := e.tokens.CountMessages(messages)
		if err != nil || estimated < 0 {
			return 0, MeasurementProvenance{}, err
		}
		total := applyMargin(estimated, margin)
		return total, MeasurementProvenance{Source: "estimate", EstimatedTail: estimated, Margin: margin}, nil
	}
	tail, err := e.tokens.CountMessages(messages[lastUsage+1:])
	if err != nil || tail < 0 {
		return 0, MeasurementProvenance{}, err
	}
	total := providerTokens + applyMargin(tail, margin)
	return total, MeasurementProvenance{Source: "provider", ProviderTokens: providerTokens, EstimatedTail: tail, Margin: margin}, nil
}

func usageTotal(usage message.TokenUsage) int {
	return usage.InputTokens + usage.CacheReadInput + usage.CacheCreationInput + usage.OutputTokens
}

func applyMargin(tokens int, margin float64) int {
	value := float64(tokens) * margin
	if value == float64(int(value)) {
		return int(value)
	}
	return int(value) + 1
}

func (e *Engine) memory(ref string) (spec.Memory, error) {
	configured, ok := e.memories[ref]
	if !ok || configured.Provider != spec.MemoryProviderBashyKB {
		return spec.Memory{}, fmt.Errorf("memory stage: invalid compiled memory %q", ref)
	}
	return configured, nil
}

func (e *Engine) append(meta Meta, kind string, data any) (event.Event, error) {
	return e.events.Append(event.Draft{SessionID: meta.SessionID, RunID: meta.RunID, StageID: meta.StageID, Type: kind, ConfigDigest: meta.ConfigDigest, Data: data})
}

func cloneMemories(in map[string]spec.Memory) map[string]spec.Memory {
	out := make(map[string]spec.Memory, len(in))
	for key, value := range in {
		value.Recall.Rings = append([]string(nil), value.Recall.Rings...)
		value.Recall.Forms = append([]string(nil), value.Recall.Forms...)
		out[key] = value
	}
	return out
}

func cloneRoutes(in map[string]spec.Route) map[string]spec.Route {
	out := make(map[string]spec.Route, len(in))
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = in[key]
	}
	return out
}

func cloneModels(in map[string]spec.Model) map[string]spec.Model {
	out := make(map[string]spec.Model, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneSources(in map[string]spec.Source) map[string]spec.Source {
	out := make(map[string]spec.Source, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
